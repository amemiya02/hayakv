package database

import (
	"strconv"
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

// execMemory handles MEMORY USAGE, MEMORY STATS, and MEMORY DOCTOR.
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
	case "STATS":
		return execMemoryStats(db.server)
	case "DOCTOR":
		return execMemoryDoctor(db.server)
	default:
		return protocol.MakeErrReply("ERR Unknown MEMORY subcommand or wrong number of arguments for '" + sub + "'")
	}
}

// execMemoryStats returns a flat array of alternating key-value pairs
// describing server memory usage, similar to real Redis MEMORY STATS.
func execMemoryStats(server *Server) redis.Reply {
	if server == nil {
		return protocol.MakeErrReply("ERR server not available")
	}

	totalAlloc := server.usedMemory()
	peak := server.peakMemory

	// Count total keys across all DBs.
	var totalKeys int
	for i := range server.dbSet {
		db := server.mustSelectDB(i)
		totalKeys += db.data.Len()
	}

	// Estimate bytes per key.
	var bytesPerKey int64
	if totalKeys > 0 {
		bytesPerKey = totalAlloc / int64(totalKeys)
	}

	// Dataset percentage (simplified: 100% when any data exists).
	var datasetPct float64
	if totalAlloc > 0 {
		datasetPct = 100.0
	}

	fields := []struct {
		key string
		val string
	}{
		{"peak.allocated", strconv.FormatInt(peak, 10)},
		{"total.allocated", strconv.FormatInt(totalAlloc, 10)},
		{"startup.allocated", "0"},
		{"replication.backlog", "0"},
		{"clients.slaves", "0"},
		{"clients.normal", "0"},
		{"cluster.links", "0"},
		{"aof.buffer", "0"},
		{"lua.caches", "0"},
		{"functions.caches", "0"},
		{"keys.count", strconv.Itoa(totalKeys)},
		{"keys.bytes-per-key", strconv.FormatInt(bytesPerKey, 10)},
		{"dataset.bytes", strconv.FormatInt(totalAlloc, 10)},
		{"dataset.percentage", strconv.FormatFloat(datasetPct, 'f', 1, 64)},
		{"allocator.resident", strconv.FormatInt(totalAlloc, 10)},
		{"allocator.active", strconv.FormatInt(totalAlloc, 10)},
		{"allocator.fragmentation.bytes", "0"},
		{"allocator.fragmentation.ratio", "1.00"},
	}

	parts := make([]redis.Reply, 0, len(fields)*2)
	for _, f := range fields {
		parts = append(parts,
			protocol.MakeBulkReply([]byte(f.key)),
			protocol.MakeBulkReply([]byte(f.val)),
		)
	}
	return protocol.MakeMultiRawReply(parts)
}

// execMemoryDoctor returns a short advisory string about memory health.
func execMemoryDoctor(server *Server) redis.Reply {
	if server == nil {
		return protocol.MakeErrReply("ERR server not available")
	}

	totalAlloc := server.usedMemory()
	var totalKeys int
	for i := range server.dbSet {
		db := server.mustSelectDB(i)
		totalKeys += db.data.Len()
	}

	var advice string
	if totalKeys == 0 {
		advice = "Hi, I'm your memory advisor. No keys in the dataset, so memory usage is minimal. Everything looks healthy!"
	} else if totalAlloc < 1024*1024 {
		advice = "Hi, I'm your memory advisor. Memory usage is under 1MB with " + strconv.Itoa(totalKeys) + " keys. No issues detected."
	} else {
		advice = "Hi, I'm your memory advisor. Total allocated: " + strconv.FormatInt(totalAlloc, 10) +
			" bytes across " + strconv.Itoa(totalKeys) + " keys. Fragmentation ratio looks normal."
	}
	return protocol.MakeBulkReply([]byte(advice))
}

func init() {
	registerCommand("Memory", execMemory, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 0, 0, 0)
}
