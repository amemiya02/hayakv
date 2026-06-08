package database

import (
	HashSet "github.com/amemiya02/hayakv/internal/datastruct/set"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"strconv"
	"strings"
)

func (db *DB) getAsSet(key string) (*object.Set, protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if !exists {
		return nil, nil
	}
	switch v := entity.Data.(type) {
	case *object.Robj:
		if v.Type != object.TypeSet {
			return nil, &protocol.WrongTypeErrReply{}
		}
		set, ok := v.Value().(*object.Set)
		if !ok {
			// Legacy HashSet.Set — convert on read
			legacySet, ok2 := v.Value().(*HashSet.Set)
			if !ok2 {
				return nil, &protocol.WrongTypeErrReply{}
			}
			newSet := object.NewSet()
			legacySet.ForEach(func(member string) bool {
				newSet.Add(member)
				return true
			})
			v.Ptr = newSet
			syncSetRobjEncoding(v, newSet)
			return newSet, nil
		}
		return set, nil
	case *HashSet.Set:
		// Legacy entity without Robj — convert
		newSet := object.NewSet()
		v.ForEach(func(member string) bool {
			newSet.Add(member)
			return true
		})
		return newSet, nil
	default:
		return nil, &protocol.WrongTypeErrReply{}
	}
}

func (db *DB) getOrInitSet(key string) (set *object.Set, inited bool, errReply protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if exists {
		switch v := entity.Data.(type) {
		case *object.Robj:
			if v.Type != object.TypeSet {
				return nil, false, &protocol.WrongTypeErrReply{}
			}
			set, ok := v.Value().(*object.Set)
			if !ok {
				// Legacy HashSet.Set stored in Robj — wrap for compatibility
				legacySet, ok2 := v.Value().(*HashSet.Set)
				if !ok2 {
					return nil, false, &protocol.WrongTypeErrReply{}
				}
				// Convert legacy set to new encoding-layer Set
				newSet := object.NewSet()
				legacySet.ForEach(func(member string) bool {
					newSet.Add(member)
					return true
				})
				v.Ptr = newSet
				syncSetRobjEncoding(v, newSet)
				return newSet, false, nil
			}
			return set, false, nil
		case *HashSet.Set:
			// Legacy entity without Robj — convert
			newSet := object.NewSet()
			v.ForEach(func(member string) bool {
				newSet.Add(member)
				return true
			})
			return newSet, false, nil
		default:
			return nil, false, &protocol.WrongTypeErrReply{}
		}
	}
	set = object.NewSet()
	robj := &object.Robj{
		Type:     object.TypeSet,
		Encoding: object.EncIntset,
		Ptr:      set,
	}
	db.PutEntity(key, &database.DataEntity{
		Data: robj,
	})
	return set, true, nil
}

// syncSetRobjEncoding updates the Robj.Encoding to match the Set's actual encoding.
func syncSetRobjEncoding(robj *object.Robj, set *object.Set) {
	robj.Encoding = set.CurrentEncoding()
}

// execSAdd adds members into set
func execSAdd(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	members := args[1:]

	// get or init entity
	set, _, errReply := db.getOrInitSet(key)
	if errReply != nil {
		return errReply
	}
	counter := 0
	for _, member := range members {
		counter += set.Add(string(member))
	}
	// Sync encoding after potential internal conversion (intset→listpack→hashtable)
	syncSetEncodingAfterWrite(db, key, set)
	db.addAof(utils.ToCmdLine3("sadd", args...))
	return protocol.MakeIntReply(int64(counter))
}

// execSIsMember checks if the given value is member of set
func execSIsMember(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	member := string(args[1])

	// get set
	set, errReply := db.getAsSet(key)
	if errReply != nil {
		return errReply
	}
	if set == nil {
		return protocol.MakeIntReply(0)
	}

	has := set.Has(member)
	if has {
		return protocol.MakeIntReply(1)
	}
	return protocol.MakeIntReply(0)
}

// execSRem removes a member from set
func execSRem(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	members := args[1:]

	set, errReply := db.getAsSet(key)
	if errReply != nil {
		return errReply
	}
	if set == nil {
		return protocol.MakeIntReply(0)
	}
	counter := 0
	for _, member := range members {
		counter += set.Remove(string(member))
	}
	if set.Len() == 0 {
		db.Remove(key)
	}
	if counter > 0 {
		db.addAof(utils.ToCmdLine3("srem", args...))
	}
	return protocol.MakeIntReply(int64(counter))
}

// execSPop removes one or more random members from set
func execSPop(db *DB, args [][]byte) redis.Reply {
	if len(args) != 1 && len(args) != 2 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'spop' command")
	}
	key := string(args[0])

	set, errReply := db.getAsSet(key)
	if errReply != nil {
		return errReply
	}
	if set == nil {
		return &protocol.NullBulkReply{}
	}

	count := 1
	if len(args) == 2 {
		count64, err := strconv.ParseInt(string(args[1]), 10, 64)
		if err != nil || count64 <= 0 {
			return protocol.MakeErrReply("ERR value is out of range, must be positive")
		}
		count = int(count64)
	}
	if count > set.Len() {
		count = set.Len()
	}

	members := set.RandomDistinctMembers(count)
	result := make([][]byte, len(members))
	for i, v := range members {
		set.Remove(v)
		result[i] = []byte(v)
	}

	if count > 0 {
		db.addAof(utils.ToCmdLine3("spop", args...))
	}
	return protocol.MakeMultiBulkReply(result)
}

// execSCard gets the number of members in a set
func execSCard(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	// get or init entity
	set, errReply := db.getAsSet(key)
	if errReply != nil {
		return errReply
	}
	if set == nil {
		return protocol.MakeIntReply(0)
	}
	return protocol.MakeIntReply(int64(set.Len()))
}

// execSMembers gets all members in a set
func execSMembers(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	// get or init entity
	set, errReply := db.getAsSet(key)
	if errReply != nil {
		return errReply
	}
	if set == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	arr := make([][]byte, set.Len())
	i := 0
	set.ForEach(func(member string) bool {
		arr[i] = []byte(member)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(arr)
}

func set2reply(set *object.Set) redis.Reply {
	arr := make([][]byte, set.Len())
	i := 0
	set.ForEach(func(member string) bool {
		arr[i] = []byte(member)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(arr)
}

// execSInter intersect multiple sets
func execSInter(db *DB, args [][]byte) redis.Reply {
	sets := make([]*object.Set, 0, len(args))
	for _, arg := range args {
		key := string(arg)
		set, errReply := db.getAsSet(key)
		if errReply != nil {
			return errReply
		}
		if set.Len() == 0 {
			return &protocol.EmptyMultiBulkReply{}
		}
		sets = append(sets, set)
	}
	result := objectIntersect(sets...)
	return set2reply(result)
}

// execSInterStore intersects multiple sets and store the result in a key
func execSInterStore(db *DB, args [][]byte) redis.Reply {
	dest := string(args[0])
	sets := make([]*object.Set, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		key := string(args[i])
		set, errReply := db.getAsSet(key)
		if errReply != nil {
			return errReply
		}
		if set.Len() == 0 {
			return protocol.MakeIntReply(0)
		}
		sets = append(sets, set)
	}
	result := objectIntersect(sets...)
	db.PutEntity(dest, &database.DataEntity{
		Data: &object.Robj{Type: object.TypeSet, Encoding: result.CurrentEncoding(), Ptr: result},
	})
	db.addAof(utils.ToCmdLine3("sinterstore", args...))
	return protocol.MakeIntReply(int64(result.Len()))
}

// execSUnion adds multiple sets
func execSUnion(db *DB, args [][]byte) redis.Reply {
	sets := make([]*object.Set, 0, len(args))
	for _, arg := range args {
		key := string(arg)
		set, errReply := db.getAsSet(key)
		if errReply != nil {
			return errReply
		}
		sets = append(sets, set)
	}
	result := objectUnion(sets...)
	return set2reply(result)
}

// execSUnionStore adds multiple sets and store the result in a key
func execSUnionStore(db *DB, args [][]byte) redis.Reply {
	dest := string(args[0])
	sets := make([]*object.Set, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		key := string(args[i])
		set, errReply := db.getAsSet(key)
		if errReply != nil {
			return errReply
		}
		sets = append(sets, set)
	}
	result := objectUnion(sets...)
	db.Remove(dest) // clean ttl
	if result.Len() == 0 {
		return protocol.MakeIntReply(0)
	}

	db.PutEntity(dest, &database.DataEntity{
		Data: &object.Robj{Type: object.TypeSet, Encoding: result.CurrentEncoding(), Ptr: result},
	})
	db.addAof(utils.ToCmdLine3("sunionstore", args...))
	return protocol.MakeIntReply(int64(result.Len()))
}

// execSDiff subtracts multiple sets
func execSDiff(db *DB, args [][]byte) redis.Reply {
	sets := make([]*object.Set, 0, len(args))
	for _, arg := range args {
		key := string(arg)
		set, errReply := db.getAsSet(key)
		if errReply != nil {
			return errReply
		}
		sets = append(sets, set)
	}
	result := objectDiff(sets...)
	return set2reply(result)
}

// execSDiffStore subtracts multiple sets and store the result in a key
func execSDiffStore(db *DB, args [][]byte) redis.Reply {
	dest := string(args[0])
	sets := make([]*object.Set, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		key := string(args[i])
		set, errReply := db.getAsSet(key)
		if errReply != nil {
			return errReply
		}
		sets = append(sets, set)
	}
	result := objectDiff(sets...)
	db.Remove(dest) // clean ttl
	if result.Len() == 0 {
		return protocol.MakeIntReply(0)
	}
	db.PutEntity(dest, &database.DataEntity{
		Data: &object.Robj{Type: object.TypeSet, Encoding: result.CurrentEncoding(), Ptr: result},
	})
	db.addAof(utils.ToCmdLine3("sdiffstore", args...))
	return protocol.MakeIntReply(int64(result.Len()))
}

// execSRandMember gets random members from set
func execSRandMember(db *DB, args [][]byte) redis.Reply {
	if len(args) != 1 && len(args) != 2 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'srandmember' command")
	}
	key := string(args[0])

	// get or init entity
	set, errReply := db.getAsSet(key)
	if errReply != nil {
		return errReply
	}
	if set == nil {
		return &protocol.NullBulkReply{}
	}
	if len(args) == 1 {
		// get a random member
		members := set.RandomMembers(1)
		return protocol.MakeBulkReply([]byte(members[0]))
	}
	count64, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	count := int(count64)
	if count > 0 {
		members := set.RandomDistinctMembers(count)
		result := make([][]byte, len(members))
		for i, v := range members {
			result[i] = []byte(v)
		}
		return protocol.MakeMultiBulkReply(result)
	} else if count < 0 {
		members := set.RandomMembers(-count)
		result := make([][]byte, len(members))
		for i, v := range members {
			result[i] = []byte(v)
		}
		return protocol.MakeMultiBulkReply(result)
	}
	return &protocol.EmptyMultiBulkReply{}
}

func execSScan(db *DB, args [][]byte) redis.Reply {
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
	key := string(args[0])
	// get entity
	set, errReply := db.getAsSet(key)
	if errReply != nil {
		return errReply
	}
	if set == nil {
		return &protocol.EmptyMultiBulkReply{}
	}
	cursor, err := strconv.Atoi(string(args[1]))
	if err != nil {
		return protocol.MakeErrReply("ERR invalid cursor")
	}

	keysReply, nextCursor := set.SetScan(cursor, count, pattern)
	if nextCursor < 0 {
		return protocol.MakeErrReply("Invalid argument")
	}

	result := make([]redis.Reply, 2)
	result[0] = protocol.MakeBulkReply([]byte(strconv.FormatInt(int64(nextCursor), 10)))
	result[1] = protocol.MakeMultiBulkReply(keysReply)

	return protocol.MakeMultiRawReply(result)
}

func init() {
	registerCommand("SAdd", execSAdd, writeFirstKey, undoSetChange, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifySet, "sadd")
	registerCommand("SIsMember", execSIsMember, readFirstKey, nil, 3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("SRem", execSRem, writeFirstKey, undoSetChange, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifySet, "srem")
	registerCommand("SPop", execSPop, writeFirstKey, undoSetChange, -2, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagRandom, redisFlagFast}, 1, 1, 1)
	registerCommand("SCard", execSCard, readFirstKey, nil, 2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("SMembers", execSMembers, readFirstKey, nil, 2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, 1, 1)
	registerCommand("SInter", execSInter, prepareSetCalculate, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, -1, 1)
	registerCommand("SInterStore", execSInterStore, prepareSetCalculateStore, rollbackFirstKey, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM}, 1, -1, 1)
	registerCommand("SUnion", execSUnion, prepareSetCalculate, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, -1, 1)
	registerCommand("SUnionStore", execSUnionStore, prepareSetCalculateStore, rollbackFirstKey, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM}, 1, -1, 1)
	registerCommand("SDiff", execSDiff, prepareSetCalculate, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, 1, 1)
	registerCommand("SDiffStore", execSDiffStore, prepareSetCalculateStore, rollbackFirstKey, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM}, 1, 1, 1)
	registerCommand("SRandMember", execSRandMember, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagRandom}, 1, 1, 1)
	registerCommand("SScan", execSScan, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagSortForScript}, 1, 1, 1)
}

// objectIntersect intersects multiple *object.Set
func objectIntersect(sets ...*object.Set) *object.Set {
	result := object.NewSet()
	if len(sets) == 0 {
		return result
	}
	countMap := make(map[string]int)
	for _, set := range sets {
		set.ForEach(func(member string) bool {
			countMap[member]++
			return true
		})
	}
	for k, v := range countMap {
		if v == len(sets) {
			result.Add(k)
		}
	}
	return result
}

// objectUnion unions multiple *object.Set
func objectUnion(sets ...*object.Set) *object.Set {
	result := object.NewSet()
	for _, set := range sets {
		set.ForEach(func(member string) bool {
			result.Add(member)
			return true
		})
	}
	return result
}

// objectDiff subtracts multiple *object.Set (first - rest)
func objectDiff(sets ...*object.Set) *object.Set {
	if len(sets) == 0 {
		return object.NewSet()
	}
	result := object.NewSet()
	sets[0].ForEach(func(member string) bool {
		result.Add(member)
		return true
	})
	for i := 1; i < len(sets); i++ {
		sets[i].ForEach(func(member string) bool {
			result.Remove(member)
			return true
		})
		if result.Len() == 0 {
			break
		}
	}
	return result
}

// syncSetEncodingAfterWrite updates the Robj.Encoding to match the Set's
// actual encoding after a write operation that may have triggered internal
// conversion (e.g. intset→listpack→hashtable).
func syncSetEncodingAfterWrite(db *DB, key string, set *object.Set) {
	entity, exists := db.GetEntity(key)
	if !exists {
		return
	}
	if robj, ok := entity.Data.(*object.Robj); ok {
		robj.Encoding = set.CurrentEncoding()
	}
}
