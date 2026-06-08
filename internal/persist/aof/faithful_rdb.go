package aof

import (
	"io"
	"strconv"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	List "github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/datastruct/set"
	SortedSet "github.com/amemiya02/hayakv/internal/datastruct/sortedset"
	"github.com/amemiya02/hayakv/internal/datastruct/stream"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/persist/rdb"
)

// rdbEngine is the read surface DumpEngineToRDB needs from the storage engine.
// *command.Server satisfies it (ForEach + GetDBSize).
type rdbEngine interface {
	ForEach(dbIndex int, cb func(key string, data *database.DataEntity, expiration *time.Time) bool)
	GetDBSize(dbIndex int) (int, int)
}

// DumpEngineToRDB walks dbCount databases of eng and writes a faithful RDB v11
// stream to w (header -> aux -> per-db SELECTDB/RESIZEDB/objects -> EOF+CRC64).
func DumpEngineToRDB(eng rdbEngine, dbCount int, w io.Writer) error {
	enc := rdb.NewEncoder(w)
	if err := enc.WriteHeader(); err != nil {
		return err
	}
	aux := [][2]string{
		{"redis-ver", "8.0.0"},
		{"redis-bits", "64"},
		{"ctime", strconv.FormatInt(time.Now().Unix(), 10)},
		{"used-mem", "0"},
		{"aof-base", "0"},
	}
	for _, kv := range aux {
		if err := enc.WriteAux(kv[0], kv[1]); err != nil {
			return err
		}
	}
	for db := 0; db < dbCount; db++ {
		keyCount, ttlCount := eng.GetDBSize(db)
		if keyCount == 0 {
			continue
		}
		if err := enc.WriteSelectDB(db); err != nil {
			return err
		}
		if err := enc.WriteResizeDB(uint64(keyCount), uint64(ttlCount)); err != nil {
			return err
		}
		var loopErr error
		eng.ForEach(db, func(key string, entity *database.DataEntity, expiration *time.Time) bool {
			loopErr = writeEntity(enc, key, entity, expiration)
			return loopErr == nil
		})
		if loopErr != nil {
			return loopErr
		}
	}
	return enc.WriteEnd()
}

func writeEntity(enc *rdb.Encoder, key string, entity *database.DataEntity, expiration *time.Time) error {
	var expireMS uint64
	if expiration != nil {
		expireMS = uint64(expiration.UnixNano() / 1e6)
	}
	keyBytes := []byte(key)
	switch obj := entity.Data.(type) {
	case []byte:
		return enc.WriteStringEntry(keyBytes, obj, expireMS)
	case List.List:
		vals := make([][]byte, 0, obj.Len())
		obj.ForEach(func(_ int, v interface{}) bool {
			b, _ := v.([]byte)
			vals = append(vals, b)
			return true
		})
		return enc.WriteListEntry(keyBytes, vals, expireMS)
	case *set.Set:
		vals := make([][]byte, 0, obj.Len())
		obj.ForEach(func(m string) bool {
			vals = append(vals, []byte(m))
			return true
		})
		return enc.WriteSetEntry(keyBytes, vals, expireMS)
	case dict.Dict:
		hash := make(map[string][]byte, obj.Len())
		obj.ForEach(func(field string, v interface{}) bool {
			b, _ := v.([]byte)
			hash[field] = b
			return true
		})
		return enc.WriteHashEntry(keyBytes, hash, expireMS)
	case *SortedSet.SortedSet:
		members := make([]rdb.ZSetMember, 0, obj.Len())
		obj.ForEachByRank(0, obj.Len(), false, func(e *SortedSet.Element) bool {
			members = append(members, rdb.ZSetMember{Member: []byte(e.Member), Score: e.Score})
			return true
		})
		return enc.WriteZSetEntry(keyBytes, members, expireMS)
	case *object.Robj:
		return writeRobjEntity(enc, keyBytes, obj, expireMS)
	default:
		return nil // unknown encodings are skipped
	}
}

// writeRobjEntity handles *object.Robj (the primary data path for string SET).
func writeRobjEntity(enc *rdb.Encoder, key []byte, robj *object.Robj, expireMS uint64) error {
	switch robj.Type {
	case object.TypeString:
		return enc.WriteStringEntry(key, robj.GetStringBytes(), expireMS)
	case object.TypeList:
		list, ok := robj.Ptr.(*object.List)
		if !ok {
			return nil
		}
		vals := make([][]byte, 0, list.Len())
		list.ForEach(func(_ int, v interface{}) bool {
			switch b := v.(type) {
			case []byte:
				vals = append(vals, b)
			case string:
				vals = append(vals, []byte(b))
			default:
				vals = append(vals, nil)
			}
			return true
		})
		return enc.WriteListEntry(key, vals, expireMS)
	case object.TypeSet:
		s, ok := robj.Ptr.(*object.Set)
		if !ok {
			return nil
		}
		vals := make([][]byte, 0, s.Len())
		s.ForEach(func(m string) bool {
			vals = append(vals, []byte(m))
			return true
		})
		return enc.WriteSetEntry(key, vals, expireMS)
	case object.TypeHash:
		h, ok := robj.Ptr.(*object.Hash)
		if !ok {
			return nil
		}
		hash := make(map[string][]byte, h.Len())
		h.ForEach(func(field string, val interface{}) bool {
			switch v := val.(type) {
			case []byte:
				hash[field] = v
			case string:
				hash[field] = []byte(v)
			default:
				hash[field] = nil
			}
			return true
		})
		return enc.WriteHashEntry(key, hash, expireMS)
	case object.TypeZSet:
		z, ok := robj.Ptr.(*object.ZSet)
		if !ok {
			return nil
		}
		members := make([]rdb.ZSetMember, 0, z.Len())
		z.ForEach(func(member string, score float64) bool {
			members = append(members, rdb.ZSetMember{Member: []byte(member), Score: score})
			return true
		})
		return enc.WriteZSetEntry(key, members, expireMS)
	case object.TypeStream:
		s, ok := robj.Ptr.(*stream.Stream)
		if !ok {
			return nil
		}
		return enc.WriteStreamEntry(key, s, expireMS)
	default:
		return nil
	}
}

// rdbLoadEngine is the write surface LoadRDBInto needs: replay decoded entries
// as the equivalent write commands so the engine's normal indexing/aof path runs.
type rdbLoadEngine interface {
	Exec(conn interface{ GetDBIndex() int }, args [][]byte) interface{}
}

// LoadEntriesAsCommands converts a decoded Entry to the multibulk command lines
// that recreate it (SET / RPUSH / SADD / HSET / ZADD / PEXPIREAT). The caller
// feeds these to the engine's Exec, exactly like AOF replay.
func LoadEntriesAsCommands(e rdb.Entry) [][]CmdLine {
	key := e.Key
	var cmds []CmdLine
	switch e.Type {
	case rdb.EntryType(0): // string
		cmds = append(cmds, CmdLine{[]byte("SET"), key, e.StringVal})
	case rdb.EntryType(1): // list
		args := append(CmdLine{[]byte("RPUSH"), key}, e.ListVal...)
		cmds = append(cmds, args)
	case rdb.EntryType(2): // set
		args := append(CmdLine{[]byte("SADD"), key}, e.SetVal...)
		cmds = append(cmds, args)
	case rdb.EntryType(4): // hash
		args := CmdLine{[]byte("HSET"), key}
		for f, v := range e.HashVal {
			args = append(args, []byte(f), v)
		}
		cmds = append(cmds, args)
	case rdb.EntryType(3): // zset
		args := CmdLine{[]byte("ZADD"), key}
		for _, m := range e.ZSetVal {
			args = append(args, []byte(strconv.FormatFloat(m.Score, 'g', -1, 64)), m.Member)
		}
		cmds = append(cmds, args)
	case rdb.EntryType(20): // stream (hayakv-internal typeStream)
		cmds = streamEntriesToCommands(string(key), e.StreamVal)
	}
	if e.ExpireMS != 0 && len(cmds) > 0 {
		cmds = append(cmds, CmdLine{[]byte("PEXPIREAT"), key, []byte(strconv.FormatUint(e.ExpireMS, 10))})
	}
	return [][]CmdLine{cmds}
}

// streamEntriesToCommands rebuilds a stream from its decoded RDB representation.
// It returns a sequence of XADD commands (one per entry) plus XGROUP CREATE and
// XSETID commands to restore group state.
func streamEntriesToCommands(key string, sd *rdb.StreamData) []CmdLine {
	if sd == nil {
		return nil
	}
	var cmds []CmdLine

	// XADD each entry with the exact stored ID
	for _, e := range sd.Entries {
		args := CmdLine{[]byte("XADD"), []byte(key), []byte(e.ID.String())}
		for _, f := range e.Fields {
			args = append(args, []byte(f[0]), []byte(f[1]))
		}
		cmds = append(cmds, args)
	}

	// Recreate consumer groups
	for _, gd := range sd.Groups {
		args := CmdLine{
			[]byte("XGROUP"), []byte("CREATE"), []byte(key),
			[]byte(gd.Name), []byte(gd.LastDelivered.String()),
		}
		cmds = append(cmds, args)

		// Restore pending entries via XCLAIM to rebuild PEL with correct delivery metadata
		for _, pe := range gd.Pending {
			args := CmdLine{
				[]byte("XCLAIM"), []byte(key), []byte(gd.Name), []byte(pe.Consumer),
				[]byte("0"), []byte(pe.ID.String()),
				[]byte("JUSTID"),
			}
			cmds = append(cmds, args)
		}
	}

	// XSETID to restore stream metadata (lastID, entriesAdded, maxDeletedID)
	if sd.EntriesAdded > 0 || sd.LastID.Ms > 0 || sd.LastID.Seq > 0 {
		args := CmdLine{
			[]byte("XSETID"), []byte(key), []byte(sd.LastID.String()),
			[]byte("ENTRIESADDED"), []byte(strconv.FormatUint(sd.EntriesAdded, 10)),
			[]byte("MAXDELETEDID"), []byte(sd.MaxDeletedID.String()),
		}
		cmds = append(cmds, args)
	}

	return cmds
}

var _ = config.Properties // bridge participates in config-gated wiring (Task 9/Task 11)
