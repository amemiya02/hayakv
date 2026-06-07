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

func init() {
	registerCommand("Object", execObject, readFirstKey, nil, 3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
}
