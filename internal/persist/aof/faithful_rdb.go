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
	"github.com/amemiya02/hayakv/internal/iface/database"
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
	default:
		return nil // unknown encodings are skipped (parity with library path)
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
	}
	if e.ExpireMS != 0 && len(cmds) > 0 {
		cmds = append(cmds, CmdLine{[]byte("PEXPIREAT"), key, []byte(strconv.FormatUint(e.ExpireMS, 10))})
	}
	return [][]CmdLine{cmds}
}

var _ = config.Properties // bridge participates in config-gated wiring (Task 9/Task 11)
