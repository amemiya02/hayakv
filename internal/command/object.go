package database

import (
	"strings"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// execObject handles OBJECT subcommands
func execObject(db *DB, args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeArgNumErrReply("object")
	}

	subCommand := strings.ToUpper(string(args[0]))
	key := string(args[1])

	switch subCommand {
	case "ENCODING":
		return execObjectEncoding(db, key)
	case "IDLETIME":
		return execObjectIdleTime(db, key)
	case "FREQ":
		return execObjectFreq(db, key)
	case "REFCOUNT":
		return execObjectRefCount(db, key)
	default:
		return protocol.MakeErrReply("ERR Unknown subcommand or wrong number of arguments for '" + subCommand + "'")
	}
}

// execObjectEncoding returns the encoding of the object stored at key
func execObjectEncoding(db *DB, key string) redis.Reply {
	entity, exists := db.GetEntity(key)
	if !exists {
		return &protocol.NullBulkReply{}
	}

	// Check if entity has Robj
	robj, ok := entity.Data.(*object.Robj)
	if !ok {
		// Legacy entity without Robj
		return protocol.MakeBulkReply([]byte("go-native"))
	}

	// Sync Robj.Encoding with the internal object's actual encoding
	// to avoid stale values after internal conversions (e.g. listpack→hashtable)
	syncRobjEncoding(robj)

	return protocol.MakeBulkReply([]byte(robj.EncodingName()))
}

// syncRobjEncoding updates robj.Encoding to match the internal object's
// actual current encoding. Internal objects may self-convert (e.g. hash
// listpack→hashtable when exceeding thresholds), but Robj.Encoding was
// never updated, causing OBJECT ENCODING to report stale values.
func syncRobjEncoding(robj *object.Robj) {
	switch robj.Type {
	case object.TypeHash:
		if hash, ok := robj.Value().(*object.Hash); ok {
			robj.Encoding = hash.CurrentEncoding()
		}
	case object.TypeList:
		if list, ok := robj.Value().(*object.List); ok {
			robj.Encoding = list.CurrentEncoding()
		}
	case object.TypeSet:
		if set, ok := robj.Value().(*object.Set); ok {
			robj.Encoding = set.CurrentEncoding()
		}
	case object.TypeZSet:
		if zset, ok := robj.Value().(*object.ZSet); ok {
			robj.Encoding = zset.CurrentEncoding()
		}
	}
}

// execObjectIdleTime returns seconds since the key was last accessed (LRU).
// Errors under an LFU policy, matching Redis.
func execObjectIdleTime(db *DB, key string) redis.Reply {
	if usingLFU() {
		return protocol.MakeErrReply("ERR An LFU maxmemory policy is selected, idle time not tracked. Please note that when switching between maxmemory policies at runtime LFU and LRU data will take some time to adjust.")
	}
	if _, exists := db.getEntityNoTouch(key); !exists {
		return protocol.MakeErrReply("ERR no such key")
	}
	raw, ok := db.lruMap.Get(key)
	if !ok {
		return protocol.MakeIntReply(0)
	}
	m := raw.(lruMeta)
	idleUnits := lruIdleFor(lruClock(), m.lruValue())
	// units are lruClockRes ms; report seconds
	return protocol.MakeIntReply(int64(idleUnits) * lruClockRes / 1000)
}

// execObjectFreq returns the logarithmic access frequency counter (LFU).
// Errors under a non-LFU policy, matching Redis.
func execObjectFreq(db *DB, key string) redis.Reply {
	if !usingLFU() {
		return protocol.MakeErrReply("ERR An LFU maxmemory policy is not selected, access frequency not tracked. Please note that when switching between maxmemory policies at runtime LFU and LRU data will take some time to adjust.")
	}
	if _, exists := db.getEntityNoTouch(key); !exists {
		return protocol.MakeErrReply("ERR no such key")
	}
	raw, ok := db.lruMap.Get(key)
	if !ok {
		return protocol.MakeIntReply(int64(lfuInitVal))
	}
	return protocol.MakeIntReply(int64(lfuDecay(raw.(lruMeta))))
}

// execObjectRefCount returns the reference count of the object at key.
// In Go (GC-managed) this always returns 1, matching single-owner semantics.
func execObjectRefCount(db *DB, key string) redis.Reply {
	if _, exists := db.getEntityNoTouch(key); !exists {
		return protocol.MakeErrReply("ERR no such key")
	}
	return protocol.MakeIntReply(1)
}

func init() {
	registerCommand("Object", execObject, readFirstKey, nil, 3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
}
