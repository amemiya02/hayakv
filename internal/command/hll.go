package database

import (
	"strconv"
	"strings"

	"github.com/amemiya02/hayakv/internal/datastruct/hll"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// getAsHLL returns the HyperLogLog stored at key, or nil if the key does not
// exist. If the key holds a value of a different type, a WRONGTYPE error is
// returned.
func (db *DB) getAsHLL(key string) (*hll.HLL, protocol.ErrorReply) {
	entity, ok := db.GetEntity(key)
	if !ok {
		return nil, nil
	}
	switch v := entity.Data.(type) {
	case *object.Robj:
		if v.Type != object.TypeString {
			return nil, &protocol.WrongTypeErrReply{}
		}
		h, ok := hll.FromBytes(v.GetStringBytes())
		if !ok {
			return nil, protocol.MakeErrReply("WRONGTYPE Key is not a valid HyperLogLog string value.")
		}
		return h, nil
	default:
		return nil, &protocol.WrongTypeErrReply{}
	}
}

// storeHLL persists the HLL bytes as a string Robj under the given key.
func (db *DB) storeHLL(key string, h *hll.HLL) {
	entity := &database.DataEntity{
		Data: &object.Robj{
			Type:     object.TypeString,
			Encoding: object.EncRaw,
			Ptr:      h.Bytes(),
		},
	}
	db.PutEntity(key, entity)
}

// execPFAdd implements PFADD key [element ...].
func execPFAdd(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	if len(args) == 1 {
		// No elements provided: create the key if it does not exist.
		_, exists := db.GetEntity(key)
		if exists {
			return protocol.MakeIntReply(0)
		}
		db.storeHLL(key, hll.New())
		db.addAof(utils.ToCmdLine3("pfadd", args...))
		return protocol.MakeIntReply(1)
	}

	// Elements provided.
	h, err := db.getAsHLL(key)
	if err != nil {
		return err
	}

	created := false
	if h == nil {
		h = hll.New()
		created = true
	}

	changed := false
	for _, elem := range args[1:] {
		if h.Add(elem) {
			changed = true
		}
	}

	if created || changed {
		db.storeHLL(key, h)
		db.addAof(utils.ToCmdLine3("pfadd", args...))
	}

	if created || changed {
		return protocol.MakeIntReply(1)
	}
	return protocol.MakeIntReply(0)
}

// execPFCount implements PFCOUNT key [key ...].
func execPFCount(db *DB, args [][]byte) redis.Reply {
	if len(args) == 1 {
		key := string(args[0])
		h, err := db.getAsHLL(key)
		if err != nil {
			return err
		}
		if h == nil {
			return protocol.MakeIntReply(0)
		}
		return protocol.MakeIntReply(int64(h.Count()))
	}

	// Multiple keys: merge into a temporary HLL.
	merged := hll.New()
	for _, arg := range args {
		key := string(arg)
		h, err := db.getAsHLL(key)
		if err != nil {
			return err
		}
		if h != nil {
			merged.Merge(h)
		}
	}
	return protocol.MakeIntReply(int64(merged.Count()))
}

// execPFMerge implements PFMERGE destkey sourcekey [sourcekey ...].
func execPFMerge(db *DB, args [][]byte) redis.Reply {
	destKey := string(args[0])

	// Load or create the destination HLL.
	dest, err := db.getAsHLL(destKey)
	if err != nil {
		return err
	}
	if dest == nil {
		dest = hll.New()
	}

	// Merge each source.
	for _, arg := range args[1:] {
		srcKey := string(arg)
		src, err := db.getAsHLL(srcKey)
		if err != nil {
			return err
		}
		if src != nil {
			dest.Merge(src)
		}
	}

	db.storeHLL(destKey, dest)
	db.addAof(utils.ToCmdLine3("pfmerge", args...))
	return &protocol.OkReply{}
}

// execPFDebug implements PFDEBUG subcommand key.
func execPFDebug(db *DB, args [][]byte) redis.Reply {
	subCmd := strings.ToUpper(string(args[0]))
	key := string(args[1])

	h, err := db.getAsHLL(key)
	if err != nil {
		return err
	}
	if h == nil {
		return protocol.MakeErrReply("ERR no such key")
	}

	switch subCmd {
	case "GETREG":
		raw := h.Bytes()
		// Return the raw register bytes (everything after the 16-byte header).
		if len(raw) <= 16 {
			return protocol.MakeBulkReply([]byte{})
		}
		return protocol.MakeBulkReply(raw[16:])

	case "ENCODING":
		raw := h.Bytes()
		if len(raw) > 5 && raw[4] == 1 {
			return protocol.MakeStatusReply("dense")
		}
		return protocol.MakeStatusReply("sparse")

	case "TODENSE":
		raw := h.Bytes()
		if len(raw) > 5 && raw[4] == 1 {
			// Already dense.
			return protocol.MakeStatusReply("dense")
		}
		// Force promotion to dense by adding many unique elements until the
		// sparse body exceeds its maximum size threshold.
		for i := 0; i < 200; i++ {
			h.Add([]byte("pfdebug-todense-pad-" + strings.Repeat("x", i%50)))
			raw = h.Bytes()
			if raw[4] == 1 {
				break
			}
		}
		db.storeHLL(key, h)
		db.addAof(utils.ToCmdLine3("pfdebug", args...))
		raw = h.Bytes()
		if raw[4] == 1 {
			return protocol.MakeStatusReply("dense")
		}
		return protocol.MakeStatusReply("sparse")

	case "DECODE":
		raw := h.Bytes()
		enc := "dense"
		if len(raw) > 5 && raw[4] == 0 {
			enc = "sparse"
		}
		count := h.Count()
		return protocol.MakeStatusReply("Encoding: " + enc + ", Registers: 16384, Count: " + strconv.FormatUint(count, 10))

	default:
		return protocol.MakeErrReply("ERR unknown PFDEBUG subcommand '" + subCmd + "'")
	}
}

// execPFSelftest implements PFSELFTEST.
func execPFSelftest(db *DB, args [][]byte) redis.Reply {
	// Basic self-test: create an HLL, add elements, verify count > 0.
	testHLL := hll.New()
	testHLL.Add([]byte("foo"))
	testHLL.Add([]byte("bar"))
	testHLL.Add([]byte("baz"))
	if testHLL.Count() == 0 {
		return protocol.MakeErrReply("ERR PFSELFTEST failed: count is 0 after adding elements")
	}

	// Test merge.
	other := hll.New()
	other.Add([]byte("quux"))
	testHLL.Merge(other)
	if testHLL.Count() == 0 {
		return protocol.MakeErrReply("ERR PFSELFTEST failed: count is 0 after merge")
	}

	// Test serialization round-trip.
	raw := testHLL.Bytes()
	restored, ok := hll.FromBytes(raw)
	if !ok {
		return protocol.MakeErrReply("ERR PFSELFTEST failed: FromBytes returned false")
	}
	if restored.Count() == 0 {
		return protocol.MakeErrReply("ERR PFSELFTEST failed: round-trip count is 0")
	}

	return &protocol.OkReply{}
}

func init() {
	registerCommand("PFAdd", execPFAdd, writeFirstKey, nil, -2, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyString, "pfadd")
	registerCommand("PFCount", execPFCount, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 1, 1, 1)
	registerCommand("PFMerge", execPFMerge, writeFirstKey, nil, -2, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM}, 1, 1, 1)
	registerCommand("PFDebug", execPFDebug, writeFirstKey, nil, -3, flagWrite).
		attachCommandExtra([]string{redisFlagAdmin}, 1, 1, 1)
	registerCommand("PFSelftest", execPFSelftest, readFirstKey, nil, 1, flagReadOnly).
		attachCommandExtra([]string{redisFlagAdmin}, 0, 0, 0)
}
