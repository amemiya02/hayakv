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

	return protocol.MakeBulkReply([]byte(robj.EncodingName()))
}

func init() {
	registerCommand("Object", execObject, readFirstKey, nil, 3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
}
