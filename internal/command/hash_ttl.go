package database

import (
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"strconv"
	"strings"
	"time"
)

// execHGetEx returns the values of the specified hash fields.
// If EX/PX/EXAT/PXAT is given, sets a TTL on those fields.
// If PERSIST is given, removes the TTL from those fields.
//
// HGETEX key [EX seconds | PX milliseconds | EXAT unix-time-seconds | PXAT unix-time-ms | PERSIST] FIELDS numfields field [field ...]
func execHGetEx(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	i := 1
	var fieldTTLms int64 // 0 = no TTL change, >0 = set, -1 = persist

	// Parse optional EX/PX/EXAT/PXAT/PERSIST before FIELDS
	for i < len(args) {
		arg := strings.ToUpper(string(args[i]))
		if arg == "FIELDS" {
			break
		}
		switch arg {
		case "EX":
			if fieldTTLms != 0 {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			sec, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || sec <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hgetex' command")
			}
			fieldTTLms = sec * 1000
			i += 2
		case "PX":
			if fieldTTLms != 0 {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			ms, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || ms <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hgetex' command")
			}
			fieldTTLms = ms
			i += 2
		case "EXAT":
			if fieldTTLms != 0 {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			sec, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || sec <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hgetex' command")
			}
			fieldTTLms = sec * 1000
			i += 2
		case "PXAT":
			if fieldTTLms != 0 {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			ms, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || ms <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hgetex' command")
			}
			fieldTTLms = ms
			i += 2
		case "PERSIST":
			if fieldTTLms != 0 {
				return &protocol.SyntaxErrReply{}
			}
			fieldTTLms = -1
			i++
		default:
			return &protocol.SyntaxErrReply{}
		}
	}

	// Parse FIELDS numfields field [field ...]
	if i >= len(args) || strings.ToUpper(string(args[i])) != "FIELDS" {
		return &protocol.SyntaxErrReply{}
	}
	i++
	if i >= len(args) {
		return &protocol.SyntaxErrReply{}
	}
	numFields, err := strconv.Atoi(string(args[i]))
	if err != nil || numFields < 0 {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	i++
	if i+numFields > len(args) {
		return &protocol.SyntaxErrReply{}
	}
	fields := make([]string, numFields)
	for j := 0; j < numFields; j++ {
		fields[j] = string(args[i+j])
	}

	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		// Key doesn't exist: return nil array
		result := make([][]byte, numFields)
		return protocol.MakeMultiBulkReply(result)
	}

	// Purge expired fields before reading
	nowMs := time.Now().UnixMilli()
	hash.PurgeExpiredFields(nowMs)

	result := make([][]byte, numFields)
	for j, field := range fields {
		raw, exists := hash.Get(field)
		if exists {
			result[j] = toBytes(raw)
		}
	}

	// Apply TTL / PERSIST to the fields
	if fieldTTLms > 0 {
		expireAtMs := nowMs + fieldTTLms
		for _, field := range fields {
			_, exists := hash.Get(field)
			if exists {
				hash.SetFieldExpire(field, expireAtMs)
			}
		}
	} else if fieldTTLms == -1 {
		for _, field := range fields {
			hash.PersistField(field)
		}
	}

	return protocol.MakeMultiBulkReply(result)
}

// execHGetDel returns the values of the specified hash fields and deletes them.
//
// HGETDEL key FIELDS numfields field [field ...]
func execHGetDel(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	i := 1

	// Parse FIELDS numfields field [field ...]
	if i >= len(args) || strings.ToUpper(string(args[i])) != "FIELDS" {
		return &protocol.SyntaxErrReply{}
	}
	i++
	if i >= len(args) {
		return &protocol.SyntaxErrReply{}
	}
	numFields, err := strconv.Atoi(string(args[i]))
	if err != nil || numFields < 0 {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	i++
	if i+numFields > len(args) {
		return &protocol.SyntaxErrReply{}
	}
	fields := make([]string, numFields)
	for j := 0; j < numFields; j++ {
		fields[j] = string(args[i+j])
	}

	hash, errReply := db.getAsHash(key)
	if errReply != nil {
		return errReply
	}
	if hash == nil {
		result := make([][]byte, numFields)
		return protocol.MakeMultiBulkReply(result)
	}

	// Purge expired fields before reading
	nowMs := time.Now().UnixMilli()
	hash.PurgeExpiredFields(nowMs)

	result := make([][]byte, numFields)
	for j, field := range fields {
		raw, exists := hash.Get(field)
		if exists {
			result[j] = toBytes(raw)
		}
	}

	// Delete the fields
	for _, field := range fields {
		hash.Remove(field)
		hash.PersistField(field) // clean up any field expiry
	}

	if hash.Len() == 0 {
		db.Remove(key)
	}
	db.addAof(utils.ToCmdLine3("hgetdel", args...))

	return protocol.MakeMultiBulkReply(result)
}

// execHSetEx sets field-value pairs in a hash with optional TTL and existence conditions.
//
// HSETEX key [FNX|FXX] [EX seconds | PX milliseconds | EXAT unix-time-seconds | PXAT unix-time-ms | KEEPTTL] FIELDS numfields field value [field value ...]
func execHSetEx(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	i := 1
	var cond string // "", "FNX", "FXX"
	var fieldTTLms int64
	keepTTL := false

	// Parse optional FNX/FXX
	for i < len(args) {
		arg := strings.ToUpper(string(args[i]))
		if arg == "FNX" || arg == "FXX" {
			if cond != "" {
				return &protocol.SyntaxErrReply{}
			}
			cond = arg
			i++
		} else {
			break
		}
	}

	// Parse optional EX/PX/EXAT/PXAT/KEEPTTL
	for i < len(args) {
		arg := strings.ToUpper(string(args[i]))
		if arg == "FIELDS" {
			break
		}
		switch arg {
		case "EX":
			if fieldTTLms != 0 || keepTTL {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			sec, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || sec <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hsetex' command")
			}
			fieldTTLms = sec * 1000
			i += 2
		case "PX":
			if fieldTTLms != 0 || keepTTL {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			ms, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || ms <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hsetex' command")
			}
			fieldTTLms = ms
			i += 2
		case "EXAT":
			if fieldTTLms != 0 || keepTTL {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			sec, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || sec <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hsetex' command")
			}
			fieldTTLms = sec * 1000
			i += 2
		case "PXAT":
			if fieldTTLms != 0 || keepTTL {
				return &protocol.SyntaxErrReply{}
			}
			if i+1 >= len(args) {
				return &protocol.SyntaxErrReply{}
			}
			ms, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil || ms <= 0 {
				return protocol.MakeErrReply("ERR invalid expire time in 'hsetex' command")
			}
			fieldTTLms = ms
			i += 2
		case "KEEPTTL":
			if fieldTTLms != 0 {
				return &protocol.SyntaxErrReply{}
			}
			keepTTL = true
			i++
		default:
			return &protocol.SyntaxErrReply{}
		}
	}

	// Parse FIELDS numfields field value [field value ...]
	if i >= len(args) || strings.ToUpper(string(args[i])) != "FIELDS" {
		return &protocol.SyntaxErrReply{}
	}
	i++
	if i >= len(args) {
		return &protocol.SyntaxErrReply{}
	}
	numFields, err := strconv.Atoi(string(args[i]))
	if err != nil || numFields < 0 {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	i++
	if i+numFields*2 > len(args) {
		return &protocol.SyntaxErrReply{}
	}

	type fieldValuePair struct {
		field string
		value []byte
	}
	pairs := make([]fieldValuePair, numFields)
	for j := 0; j < numFields; j++ {
		pairs[j] = fieldValuePair{
			field: string(args[i+j*2]),
			value: args[i+j*2+1],
		}
	}

	hash, inited, errReply := db.getOrInitHash(key)
	if errReply != nil {
		return errReply
	}

	nowMs := time.Now().UnixMilli()
	hash.PurgeExpiredFields(nowMs)

	setCount := int64(0)
	switch cond {
	case "FNX":
		for _, p := range pairs {
			_, exists := hash.Get(p.field)
			if !exists {
				hash.Put(p.field, p.value)
				if fieldTTLms > 0 {
					hash.SetFieldExpire(p.field, nowMs+fieldTTLms)
				}
				setCount++
			}
		}
	case "FXX":
		for _, p := range pairs {
			_, exists := hash.Get(p.field)
			if exists {
				hash.Put(p.field, p.value)
				if !keepTTL && fieldTTLms > 0 {
					hash.SetFieldExpire(p.field, nowMs+fieldTTLms)
				} else if !keepTTL && fieldTTLms == 0 {
					hash.PersistField(p.field)
				}
				setCount++
			}
		}
	default:
		for _, p := range pairs {
			hash.Put(p.field, p.value)
			if fieldTTLms > 0 {
				hash.SetFieldExpire(p.field, nowMs+fieldTTLms)
			}
			setCount++
		}
	}

	if setCount > 0 || inited {
		db.addAof(utils.ToCmdLine3("hsetex", args...))
	}

	return protocol.MakeIntReply(setCount)
}

// parseHExpireArgs parses the common argument pattern for HEXPIRE family commands.
// Expected after key: [NX|XX|GT|LT] FIELDS numfields field [field ...]
// Returns: option, fields, error reply.  args starts at the first arg after the key.
func parseHExpireArgs(args [][]byte) (int, []string, protocol.ErrorReply) {
	idx := 0

	// Check for optional NX/XX/GT/LT
	option := 0
	if idx < len(args) {
		opt := strings.ToUpper(string(args[idx]))
		switch opt {
		case "NX":
			option = expireOptNX
			idx++
		case "XX":
			option = expireOptXX
			idx++
		case "GT":
			option = expireOptGT
			idx++
		case "LT":
			option = expireOptLT
			idx++
		}
	}

	// Parse FIELDS keyword
	if idx >= len(args) || strings.ToUpper(string(args[idx])) != "FIELDS" {
		return 0, nil, &protocol.SyntaxErrReply{}
	}
	idx++

	// Parse numfields
	if idx >= len(args) {
		return 0, nil, &protocol.SyntaxErrReply{}
	}
	numFields, err := strconv.Atoi(string(args[idx]))
	if err != nil || numFields < 0 {
		return 0, nil, protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	idx++

	// Parse field names
	if idx+numFields > len(args) {
		return 0, nil, protocol.MakeErrReply("ERR number of fields and number of arguments mismatch")
	}
	fields := make([]string, numFields)
	for i := 0; i < numFields; i++ {
		fields[i] = string(args[idx+i])
	}

	return option, fields, nil
}

// parseHFieldsArgs parses the argument pattern for HTTL/HPTTL/HEXPIRETIME/HPEXPIRETIME/HPERSIST.
// Expected: key FIELDS numfields field [field ...]
func parseHFieldsArgs(args [][]byte) (string, []string, protocol.ErrorReply) {
	key := string(args[0])

	if len(args) < 2 || strings.ToUpper(string(args[1])) != "FIELDS" {
		return "", nil, &protocol.SyntaxErrReply{}
	}
	if len(args) < 3 {
		return "", nil, &protocol.SyntaxErrReply{}
	}
	numFields, err := strconv.Atoi(string(args[2]))
	if err != nil || numFields < 0 {
		return "", nil, protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	if 3+numFields > len(args) {
		return "", nil, protocol.MakeErrReply("ERR number of fields and number of arguments mismatch")
	}
	fields := make([]string, numFields)
	for i := 0; i < numFields; i++ {
		fields[i] = string(args[3+i])
	}

	return key, fields, nil
}

// checkHFieldExpireOption checks the NX/XX/GT/LT option for hash field expiry.
func checkHFieldExpireOption(hash *object.Hash, field string, option int, expireAtMs int64) bool {
	if option == 0 {
		return true
	}
	currentExp, hasTTL := hash.FieldExpire(field)
	if option&expireOptNX != 0 {
		if hasTTL {
			return false
		}
	}
	if option&expireOptXX != 0 {
		if !hasTTL {
			return false
		}
	}
	if option&expireOptGT != 0 {
		if !hasTTL {
			return true
		}
		if expireAtMs <= currentExp {
			return false
		}
	}
	if option&expireOptLT != 0 {
		if !hasTTL {
			return true
		}
		if expireAtMs >= currentExp {
			return false
		}
	}
	return true
}

// hExpireFields sets expiry on hash fields and returns per-field results.
// expireAtMs is the absolute expiry time in milliseconds.
func hExpireFields(hash *object.Hash, fields []string, option int, expireAtMs int64) redis.Reply {
	nowMs := time.Now().UnixMilli()
	if hash.HasFieldExpiries() {
		hash.PurgeExpiredFields(nowMs)
	}

	results := make([]int64, len(fields))
	for i, field := range fields {
		_, exists := hash.Get(field)
		if !exists {
			results[i] = -2 // no such field
			continue
		}

		if !checkHFieldExpireOption(hash, field, option, expireAtMs) {
			results[i] = 0 // condition unmet
			continue
		}

		hash.SetFieldExpire(field, expireAtMs)
		results[i] = 1 // set
	}

	replies := make([]redis.Reply, len(results))
	for i, r := range results {
		replies[i] = protocol.MakeIntReply(r)
	}
	return protocol.MakeMultiRawReply(replies)
}

// execHExpire sets expiry in seconds on hash fields.
// HEXPIRE key seconds [NX|XX|GT|LT] FIELDS numfields field [field ...]
func execHExpire(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	seconds, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	option, fields, errReply := parseHExpireArgs(args[2:])
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}
	if hash == nil {
		results := make([]redis.Reply, len(fields))
		for i := range fields {
			results[i] = protocol.MakeIntReply(-2)
		}
		return protocol.MakeMultiRawReply(results)
	}

	expireAtMs := time.Now().UnixMilli() + seconds*1000
	db.addAof(utils.ToCmdLine3("hexpire", args...))
	return hExpireFields(hash, fields, option, expireAtMs)
}

// execHPExpire sets expiry in milliseconds on hash fields.
// HPEXPIRE key ms [NX|XX|GT|LT] FIELDS numfields field [field ...]
func execHPExpire(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	ms, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	option, fields, errReply := parseHExpireArgs(args[2:])
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}
	if hash == nil {
		results := make([]redis.Reply, len(fields))
		for i := range fields {
			results[i] = protocol.MakeIntReply(-2)
		}
		return protocol.MakeMultiRawReply(results)
	}

	expireAtMs := time.Now().UnixMilli() + ms
	db.addAof(utils.ToCmdLine3("hpexpire", args...))
	return hExpireFields(hash, fields, option, expireAtMs)
}

// execHExpireAt sets expiry as absolute unix timestamp in seconds on hash fields.
// HEXPIREAT key unix-time-seconds [NX|XX|GT|LT] FIELDS numfields field [field ...]
func execHExpireAt(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	unixSec, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	option, fields, errReply := parseHExpireArgs(args[2:])
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}
	if hash == nil {
		results := make([]redis.Reply, len(fields))
		for i := range fields {
			results[i] = protocol.MakeIntReply(-2)
		}
		return protocol.MakeMultiRawReply(results)
	}

	expireAtMs := unixSec * 1000
	db.addAof(utils.ToCmdLine3("hexpireat", args...))
	return hExpireFields(hash, fields, option, expireAtMs)
}

// execHPExpireAt sets expiry as absolute unix timestamp in milliseconds on hash fields.
// HPEXPIREAT key unix-time-ms [NX|XX|GT|LT] FIELDS numfields field [field ...]
func execHPExpireAt(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	unixMs, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	option, fields, errReply := parseHExpireArgs(args[2:])
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}
	if hash == nil {
		results := make([]redis.Reply, len(fields))
		for i := range fields {
			results[i] = protocol.MakeIntReply(-2)
		}
		return protocol.MakeMultiRawReply(results)
	}

	db.addAof(utils.ToCmdLine3("hpexpireat", args...))
	return hExpireFields(hash, fields, option, unixMs)
}

// execHTTL returns TTL in seconds for hash fields.
// HTTL key FIELDS numfields field [field ...]
func execHTTL(db *DB, args [][]byte) redis.Reply {
	key, fields, errReply := parseHFieldsArgs(args)
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}

	nowMs := time.Now().UnixMilli()
	if hash != nil && hash.HasFieldExpiries() {
		hash.PurgeExpiredFields(nowMs)
	}

	results := make([]redis.Reply, len(fields))
	for i, field := range fields {
		if hash == nil {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		_, exists := hash.Get(field)
		if !exists {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		expMs, hasTTL := hash.FieldExpire(field)
		if !hasTTL {
			results[i] = protocol.MakeIntReply(-1)
			continue
		}
		ttlMs := expMs - nowMs
		if ttlMs <= 0 {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		results[i] = protocol.MakeIntReply(ttlMs / 1000)
	}
	return protocol.MakeMultiRawReply(results)
}

// execHPTTL returns TTL in milliseconds for hash fields.
// HPTTL key FIELDS numfields field [field ...]
func execHPTTL(db *DB, args [][]byte) redis.Reply {
	key, fields, errReply := parseHFieldsArgs(args)
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}

	nowMs := time.Now().UnixMilli()
	if hash != nil && hash.HasFieldExpiries() {
		hash.PurgeExpiredFields(nowMs)
	}

	results := make([]redis.Reply, len(fields))
	for i, field := range fields {
		if hash == nil {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		_, exists := hash.Get(field)
		if !exists {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		expMs, hasTTL := hash.FieldExpire(field)
		if !hasTTL {
			results[i] = protocol.MakeIntReply(-1)
			continue
		}
		ttlMs := expMs - nowMs
		if ttlMs <= 0 {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		results[i] = protocol.MakeIntReply(ttlMs)
	}
	return protocol.MakeMultiRawReply(results)
}

// execHExpireTime returns absolute expiry timestamp in seconds for hash fields.
// HEXPIRETIME key FIELDS numfields field [field ...]
func execHExpireTime(db *DB, args [][]byte) redis.Reply {
	key, fields, errReply := parseHFieldsArgs(args)
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}

	nowMs := time.Now().UnixMilli()
	if hash != nil && hash.HasFieldExpiries() {
		hash.PurgeExpiredFields(nowMs)
	}

	results := make([]redis.Reply, len(fields))
	for i, field := range fields {
		if hash == nil {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		_, exists := hash.Get(field)
		if !exists {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		expMs, hasTTL := hash.FieldExpire(field)
		if !hasTTL {
			results[i] = protocol.MakeIntReply(-1)
			continue
		}
		results[i] = protocol.MakeIntReply(expMs / 1000)
	}
	return protocol.MakeMultiRawReply(results)
}

// execHPExpireTime returns absolute expiry timestamp in milliseconds for hash fields.
// HPEXPIRETIME key FIELDS numfields field [field ...]
func execHPExpireTime(db *DB, args [][]byte) redis.Reply {
	key, fields, errReply := parseHFieldsArgs(args)
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}

	nowMs := time.Now().UnixMilli()
	if hash != nil && hash.HasFieldExpiries() {
		hash.PurgeExpiredFields(nowMs)
	}

	results := make([]redis.Reply, len(fields))
	for i, field := range fields {
		if hash == nil {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		_, exists := hash.Get(field)
		if !exists {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		expMs, hasTTL := hash.FieldExpire(field)
		if !hasTTL {
			results[i] = protocol.MakeIntReply(-1)
			continue
		}
		results[i] = protocol.MakeIntReply(expMs)
	}
	return protocol.MakeMultiRawReply(results)
}

// execHPersist removes TTL from hash fields.
// HPERSIST key FIELDS numfields field [field ...]
func execHPersist(db *DB, args [][]byte) redis.Reply {
	key, fields, errReply := parseHFieldsArgs(args)
	if errReply != nil {
		return errReply
	}

	hash, hashErr := db.getAsHash(key)
	if hashErr != nil {
		return hashErr
	}

	nowMs := time.Now().UnixMilli()
	if hash != nil && hash.HasFieldExpiries() {
		hash.PurgeExpiredFields(nowMs)
	}

	results := make([]redis.Reply, len(fields))
	for i, field := range fields {
		if hash == nil {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		_, exists := hash.Get(field)
		if !exists {
			results[i] = protocol.MakeIntReply(-2)
			continue
		}
		_, hasTTL := hash.FieldExpire(field)
		if !hasTTL {
			results[i] = protocol.MakeIntReply(-1)
			continue
		}
		hash.PersistField(field)
		results[i] = protocol.MakeIntReply(1)
	}

	db.addAof(utils.ToCmdLine3("hpersist", args...))
	return protocol.MakeMultiRawReply(results)
}

func init() {
	registerCommand("HGetEx", execHGetEx, readFirstKey, nil, -5, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HGetDel", execHGetDel, writeFirstKey, nil, -4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hdel")
	registerCommand("HSetEx", execHSetEx, writeFirstKey, nil, -5, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hset")
	registerCommand("HExpire", execHExpire, writeFirstKey, nil, -5, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hexpire")
	registerCommand("HPExpire", execHPExpire, writeFirstKey, nil, -5, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hpexpire")
	registerCommand("HExpireAt", execHExpireAt, writeFirstKey, nil, -5, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hexpireat")
	registerCommand("HPExpireAt", execHPExpireAt, writeFirstKey, nil, -5, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hpexpireat")
	registerCommand("HTTL", execHTTL, readFirstKey, nil, -4, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HPTTL", execHPTTL, readFirstKey, nil, -4, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HExpireTime", execHExpireTime, readFirstKey, nil, -4, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HPExpireTime", execHPExpireTime, readFirstKey, nil, -4, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("HPersist", execHPersist, writeFirstKey, nil, -4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyHash, "hpersist")
}
