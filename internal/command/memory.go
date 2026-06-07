package database

import (
	"strings"

	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/datastruct/set"
	"github.com/amemiya02/hayakv/internal/datastruct/sortedset"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// Per-entry/per-key overhead constants for the approximate memory model. These
// are deliberate, deterministic estimates (NOT real Redis jemalloc sizes) so the
// accounting is reproducible for tests; exact bytes are non-diffable vs Redis.
const (
	perKeyOverhead  = 56 // dictEntry + key sds header approximation
	perElemOverhead = 16 // per collection element bookkeeping
	robjOverhead    = 16 // robj header
)

// estimateEntitySize returns an approximate byte cost of storing key->entity.
func estimateEntitySize(key string, entity *database.DataEntity) int64 {
	size := int64(perKeyOverhead + len(key))
	if entity == nil {
		return size
	}
	size += valueSize(entity.Data)
	return size
}

func valueSize(v interface{}) int64 {
	switch d := v.(type) {
	case []byte:
		return int64(len(d) + 8)
	case *object.Robj:
		return robjOverhead + valueSize(d.Ptr)
	case int64:
		return 8
	case list.List:
		var total int64
		d.ForEach(func(i int, val interface{}) bool {
			if b, ok := val.([]byte); ok {
				total += int64(len(b)) + perElemOverhead
			} else {
				total += perElemOverhead
			}
			return true
		})
		return total
	case dict.Dict:
		var total int64
		d.ForEach(func(field string, val interface{}) bool {
			total += int64(len(field)) + perElemOverhead
			if b, ok := val.([]byte); ok {
				total += int64(len(b))
			}
			return true
		})
		return total
	case *set.Set:
		var total int64
		d.ForEach(func(member string) bool {
			total += int64(len(member)) + perElemOverhead
			return true
		})
		return total
	case *sortedset.SortedSet:
		var total int64
		d.ForEachByRank(0, d.Len(), true, func(e *sortedset.Element) bool {
			total += int64(len(e.Member)) + perElemOverhead + 8 // +score
			return true
		})
		return total
	default:
		return perElemOverhead
	}
}

// execMemory handles MEMORY USAGE key [SAMPLES n]. Other MEMORY subcommands are
// not supported.
func execMemory(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeArgNumErrReply("memory")
	}
	sub := strings.ToUpper(string(args[0]))
	switch sub {
	case "USAGE":
		if len(args) < 2 {
			return protocol.MakeArgNumErrReply("memory|usage")
		}
		key := string(args[1])
		entity, ok := db.GetEntity(key)
		if !ok {
			return &protocol.NullBulkReply{}
		}
		size := estimateEntitySize(key, entity)
		return protocol.MakeIntReply(size)
	default:
		return protocol.MakeErrReply("ERR Unknown MEMORY subcommand or wrong number of arguments for '" + sub + "'")
	}
}

func init() {
	registerCommand("Memory", execMemory, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 0, 0, 0)
}
