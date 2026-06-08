package database

import (
	Dict "github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"strconv"
	"strings"
)

// getAsHash returns the Hash object for the given key, or nil if key does not exist.
// Returns WrongTypeErrReply if the key is not a hash type.
func (db *DB) getAsHash(key string) (*object.Hash, protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if !exists {
		return nil, nil
	}
	switch v := entity.Data.(type) {
	case *object.Robj:
		if v.Type != object.TypeHash {
			return nil, &protocol.WrongTypeErrReply{}
		}
		return v.Value().(*object.Hash), nil
	case *object.Hash:
		return v, nil
	case Dict.Dict:
		// RDB loader stores hash as Dict.Dict; wrap in Hash for unified access
		return object.NewHashFromDict(v), nil
	default:
		return nil, &protocol.WrongTypeErrReply{}
	}
}

// getAsDict returns the hash as a Dict for backward compatibility.
// This is used by tx_utils.go and should not be used by new code.
func (db *DB) getAsDict(key string) (Dict.Dict, protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if !exists {
		return nil, nil
	}
	switch v := entity.Data.(type) {
	case *object.Robj:
		if v.Type != object.TypeHash {
			return nil, &protocol.WrongTypeErrReply{}
		}
		hash := v.Value().(*object.Hash)
		return hash.GetAsDict(), nil
	case *object.Hash:
		return v.GetAsDict(), nil
	case Dict.Dict:
		return v, nil
	default:
		return nil, &protocol.WrongTypeErrReply{}
	}
}

// getOrInitHash returns the Hash object for the given key, creating it if it does not exist.
// Returns inited=true if a new hash was created.
func (db *DB) getOrInitHash(key string) (hash *object.Hash, inited bool, errReply protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if exists {
		switch v := entity.Data.(type) {
		case *object.Robj:
			if v.Type != object.TypeHash {
				return nil, false, &protocol.WrongTypeErrReply{}
			}
			return v.Value().(*object.Hash), false, nil
		case *object.Hash:
			return v, false, nil
		default:
			return nil, false, &protocol.WrongTypeErrReply{}
		}
	}
	// Create new hash with Robj wrapper
	hash = object.NewHash()
	robj := &object.Robj{
		Type:     object.TypeHash,
		Encoding: object.EncListpack,
		Ptr:      hash,
	}
	db.PutEntity(key, &database.DataEntity{
		Data: robj,
	})
	return hash, true, nil
}

// execHSet sets one or more field/value pairs in a hash
// HSET key field value [field value ...]
func execHSet(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 || len(args)%2 != 1 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])

	// get or init entity
	hash, _, errReply := db.getOrInitHash(key)
	if errReply != nil {
		return errReply
	}

	result := 0
	for i := 1; i < len(args); i += 2 {
		field := string(args[i])
		value := args[i+1]
		result += hash.Put(field, value)
	}
	db.addAof(utils.ToCmdLine3("hset", args...))
	return protocol.MakeIntReply(int64(result))
}

func undoHSet(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	size := (len(args) - 1) / 2
	fields := make([]string, size)
	for i := 0; i < size; i++ {
		fields[i] = string(args[2*i+1])
	}
	return rollbackHashFields(db, key, fields...)
}

// execHSetNX sets field in hash table only if field not exists
func execHSetNX(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	field := string(args[1])
	value := args[2]

	hash, _, errReply := db.getOrInitHash(key)
	if errReply != nil {
		return errReply
	}

	result := hash.PutIfAbsent(field, value)
	if result > 0 {
		db.addAof(utils.ToCmdLine3("hsetnx", args...))

	}
	return protocol.MakeIntReply(int64(result))
}

// execHGet gets field value of hash table
func execHGet(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	field := string(args[1])

	// get entity
	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return &protocol.NullBulkReply{}
	}

	raw, exists := hash.Get(field)
	if !exists {
		return &protocol.NullBulkReply{}
	}
	value := toBytes(raw)
	return protocol.MakeBulkReply(value)
}

// execHExists checks if a hash field exists
func execHExists(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	field := string(args[1])

	// get entity
	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return protocol.MakeIntReply(0)
	}

	_, exists := hash.Get(field)
	if exists {
		return protocol.MakeIntReply(1)
	}
	return protocol.MakeIntReply(0)
}

// execHDel deletes a hash field
func execHDel(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	fields := make([]string, len(args)-1)
	fieldArgs := args[1:]
	for i, v := range fieldArgs {
		fields[i] = string(v)
	}

	// get entity
	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return protocol.MakeIntReply(0)
	}

	deleted := 0
	for _, field := range fields {
		_, result := hash.Remove(field)
		deleted += result
	}
	if hash.Len() == 0 {
		db.Remove(key)
	}
	if deleted > 0 {
		db.addAof(utils.ToCmdLine3("hdel", args...))
	}

	return protocol.MakeIntReply(int64(deleted))
}

func undoHDel(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	fields := make([]string, len(args)-1)
	fieldArgs := args[1:]
	for i, v := range fieldArgs {
		fields[i] = string(v)
	}
	return rollbackHashFields(db, key, fields...)
}

// execHLen gets number of fields in hash table
func execHLen(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])

	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return protocol.MakeIntReply(0)
	}
	return protocol.MakeIntReply(int64(hash.Len()))
}

// execHStrlen Returns the string length of the value associated with field in the hash stored at key.
// If the key or the field do not exist, 0 is returned.
func execHStrlen(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	field := string(args[1])

	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return protocol.MakeIntReply(0)
	}

	raw, exists := hash.Get(field)
	if exists {
		value := toBytes(raw)
		return protocol.MakeIntReply(int64(len(value)))
	}
	return protocol.MakeIntReply(0)
}

// execHMSet sets multi fields in hash table
func execHMSet(db *DB, args [][]byte) redis.Reply {
	// parse args
	if len(args)%2 != 1 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	size := (len(args) - 1) / 2
	fields := make([]string, size)
	values := make([][]byte, size)
	for i := 0; i < size; i++ {
		fields[i] = string(args[2*i+1])
		values[i] = args[2*i+2]
	}

	// get or init entity
	hash, _, errReply := db.getOrInitHash(key)
	if errReply != nil {
		return errReply
	}

	// put data
	for i, field := range fields {
		value := values[i]
		hash.Put(field, value)
	}
	db.addAof(utils.ToCmdLine3("hmset", args...))
	return &protocol.OkReply{}
}

func undoHMSet(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	size := (len(args) - 1) / 2
	fields := make([]string, size)
	for i := 0; i < size; i++ {
		fields[i] = string(args[2*i+1])
	}
	return rollbackHashFields(db, key, fields...)
}

// execHMGet gets multi fields in hash table
func execHMGet(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	size := len(args) - 1
	fields := make([]string, size)
	for i := 0; i < size; i++ {
		fields[i] = string(args[i+1])
	}

	// get entity
	result := make([][]byte, size)
	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return protocol.MakeMultiBulkReply(result)
	}

	for i, field := range fields {
		value, ok := hash.Get(field)
		if !ok {
			result[i] = nil
		} else {
			result[i] = toBytes(value)
		}
	}
	return protocol.MakeMultiBulkReply(result)
}

// execHKeys gets all field names in hash table
func execHKeys(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	keys := hash.Keys()
	fields := make([][]byte, len(keys))
	for i, k := range keys {
		fields[i] = []byte(k)
	}
	return protocol.MakeMultiBulkReply(fields)
}

// execHVals gets all field value in hash table
func execHVals(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	// get entity
	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	values := make([][]byte, 0, hash.Len())
	hash.ForEach(func(field string, val interface{}) bool {
		values = append(values, toBytes(val))
		return true
	})
	return protocol.MakeMultiBulkReply(values)
}

// execHGetAll gets all key-value entries in hash table
func execHGetAll(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	// get entity
	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	size := hash.Len()
	result := make([][]byte, 0, size*2)
	hash.ForEach(func(field string, val interface{}) bool {
		result = append(result, []byte(field))
		result = append(result, toBytes(val))
		return true
	})
	return protocol.MakeMultiBulkReply(result)
}

// execHIncrBy increments the integer value of a hash field by the given number
func execHIncrBy(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	field := string(args[1])
	rawDelta := string(args[2])
	delta, err := strconv.ParseInt(rawDelta, 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	hash, _, errReply := db.getOrInitHash(key)
	if errReply != nil {
		return errReply
	}

	value, exists := hash.Get(field)
	if !exists {
		hash.Put(field, args[2])
		db.addAof(utils.ToCmdLine3("hincrby", args...))
		return protocol.MakeBulkReply(args[2])
	}
	val, err := strconv.ParseInt(string(toBytes(value)), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR hash value is not an integer")
	}
	val += delta
	bytes := []byte(strconv.FormatInt(val, 10))
	hash.Put(field, bytes)
	db.addAof(utils.ToCmdLine3("hincrby", args...))
	return protocol.MakeBulkReply(bytes)
}

func undoHIncr(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	field := string(args[1])
	return rollbackHashFields(db, key, field)
}

// execHIncrByFloat increments the float value of a hash field by the given number
func execHIncrByFloat(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	field := string(args[1])
	rawDelta := string(args[2])
	delta, err := strconv.ParseFloat(rawDelta, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not a valid float")
	}

	// get or init entity
	hash, _, errReply := db.getOrInitHash(key)
	if errReply != nil {
		return errReply
	}

	value, exists := hash.Get(field)
	if !exists {
		hash.Put(field, args[2])
		db.addAof(utils.ToCmdLine3("hincrbyfloat", args...))
		return protocol.MakeBulkReply(args[2])
	}
	val, err := strconv.ParseFloat(string(toBytes(value)), 64)
	if err != nil {
		return protocol.MakeErrReply("ERR hash value is not a float")
	}
	result := val + delta
	resultBytes := []byte(strconv.FormatFloat(result, 'f', -1, 64))
	hash.Put(field, resultBytes)
	db.addAof(utils.ToCmdLine3("hincrbyfloat", args...))
	return protocol.MakeBulkReply(resultBytes)
}

// execHRandField return a random field(or field-value) from the hash value stored at key.
func execHRandField(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	count := 1
	withvalues := 0

	if len(args) > 3 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'hrandfield' command")
	}

	if len(args) == 3 {
		if strings.ToLower(string(args[2])) == "withvalues" {
			withvalues = 1
		} else {
			return protocol.MakeSyntaxErrReply()
		}
	}

	if len(args) >= 2 {
		count64, err := strconv.ParseInt(string(args[1]), 10, 64)
		if err != nil {
			return protocol.MakeErrReply("ERR value is not an integer or out of range")
		}
		count = int(count64)
	}

	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	if count > 0 {
		fields := hash.RandomDistinctKeys(count)
		numField := len(fields)
		if withvalues == 0 {
			result := make([][]byte, numField)
			for i, v := range fields {
				result[i] = []byte(v)
			}
			return protocol.MakeMultiBulkReply(result)
		} else {
			result := make([][]byte, 2*numField)
			for i, v := range fields {
				result[2*i] = []byte(v)
				raw, _ := hash.Get(v)
				result[2*i+1] = toBytes(raw)
			}
			return protocol.MakeMultiBulkReply(result)
		}
	} else if count < 0 {
		fields := hash.RandomKeys(-count)
		numField := len(fields)
		if withvalues == 0 {
			result := make([][]byte, numField)
			for i, v := range fields {
				result[i] = []byte(v)
			}
			return protocol.MakeMultiBulkReply(result)
		} else {
			result := make([][]byte, 2*numField)
			for i, v := range fields {
				result[2*i] = []byte(v)
				raw, _ := hash.Get(v)
				result[2*i+1] = toBytes(raw)
			}
			return protocol.MakeMultiBulkReply(result)
		}
	}

	// 'count' is 0 will reach.
	return &protocol.EmptyMultiBulkReply{}
}

func execHScan(db *DB, args [][]byte) redis.Reply {
	var count int = 10
	var pattern string = "*"
	if len(args) > 2 {
		for i := 2; i < len(args); i++ {
			arg := strings.ToLower(string(args[i]))
			if arg == "count" {
				count0, err := strconv.Atoi(string(args[i+1]))
				if err != nil {
					return &protocol.SyntaxErrReply{}
				}
				count = count0
				i++
			} else if arg == "match" {
				pattern = string(args[i+1])
				i++
			} else {
				return &protocol.SyntaxErrReply{}
			}
		}
	}
	if len(args) < 2 {
		return &protocol.SyntaxErrReply{}
	}
	key := string(args[0])
	// get entity
	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		return &protocol.NullBulkReply{}
	}
	cursor, err := strconv.Atoi(string(args[1]))
	if err != nil {
		return protocol.MakeErrReply("ERR invalid cursor")
	}

	keysReply, nextCursor := hash.Scan(cursor, count, pattern)
	if nextCursor < 0 {
		return protocol.MakeErrReply("Invalid argument")
	}

	result := make([]redis.Reply, 2)
	result[0] = protocol.MakeBulkReply([]byte(strconv.FormatInt(int64(nextCursor), 10)))
	result[1] = protocol.MakeMultiBulkReply(keysReply)

	return protocol.MakeMultiRawReply(result)
}

// toBytes converts an interface{} value to []byte.
// Handles []byte, string, and int64 types.
func toBytes(val interface{}) []byte {
	switch v := val.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	case int64:
		return []byte(strconv.FormatInt(v, 10))
	default:
		return nil
	}
}

func init() {
	registerCommand("HSet", execHSet, writeFirstKey, undoHSet, -4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hset")
	registerCommand("HSetNX", execHSetNX, writeFirstKey, undoHSet, 4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hset")
	registerCommand("HGet", execHGet, readFirstKey, nil, 3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HExists", execHExists, readFirstKey, nil, 3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HDel", execHDel, writeFirstKey, undoHDel, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hdel")
	registerCommand("HLen", execHLen, readFirstKey, nil, 2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HStrlen", execHStrlen, readFirstKey, nil, 3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HMSet", execHMSet, writeFirstKey, undoHMSet, -4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hset")
	registerCommand("HMGet", execHMGet, readFirstKey, nil, -3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HKeys", execHKeys, readFirstKey, nil, 2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, 1, 1)
	registerCommand("HVals", execHVals, readFirstKey, nil, 2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, 1, 1)
	registerCommand("HGetAll", execHGetAll, readFirstKey, nil, 2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagRandom}, 1, 1, 1)
	registerCommand("HIncrBy", execHIncrBy, writeFirstKey, undoHIncr, 4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hincrby")
	registerCommand("HIncrByFloat", execHIncrByFloat, writeFirstKey, undoHIncr, 4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hincrbyfloat")
	registerCommand("HRandField", execHRandField, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagRandom, redisFlagReadonly}, 1, 1, 1)
	registerCommand("HScan", execHScan, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, 1, 1)
}
