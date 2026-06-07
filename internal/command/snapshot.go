// Package database implements point-in-time snapshot for non-blocking BGSAVE.
//
// Divergence from C Redis: C Redis does BGSAVE by fork() — the child inherits a
// copy-on-write snapshot of the entire heap. Go cannot do this because the
// runtime is multi-threaded (GC, scheduler, netpoller); calling fork() and then
// doing anything other than exec() is undefined. Instead, hayakv takes a
// point-in-time snapshot in-process: under the single-threaded event loop the
// keyspace is quiescent between command executions, so we cheaply copy each
// key's (key, raw-value, expire) triple into a flat []rdb.Entry slice, hand
// that immutable slice to a background goroutine, and encode it to disk there.
// The copy is O(keys) and shallow for strings, deep for aggregates.
package database

import (
	"io"
	"os"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	List "github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/datastruct/set"
	SortedSet "github.com/amemiya02/hayakv/internal/datastruct/sortedset"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/persist/rdb"
)

// dbSnapshot is one database's keys materialized into an immutable slice of
// rdb.Entry. It is detached from the live engine, so a background goroutine can
// encode it while the loop keeps mutating the real db.
type dbSnapshot struct {
	dbIndex int
	entries []rdb.Entry
}

// snapshotAllDBs copies every key of every database into rdb.Entry form. Because
// hayakv executes commands single-threaded (M4), no key changes during this copy,
// so the result is a consistent point-in-time view WITHOUT fork/COW.
func (server *Server) snapshotAllDBs() []dbSnapshot {
	snaps := make([]dbSnapshot, 0, len(server.dbSet))
	for db := 0; db < len(server.dbSet); db++ {
		var entries []rdb.Entry
		server.ForEach(db, func(key string, entity *database.DataEntity, expiration *time.Time) bool {
			e := rdb.Entry{DBIndex: db, Key: []byte(key)}
			if expiration != nil {
				e.ExpireMS = uint64(expiration.UnixNano() / 1e6)
			}
			if snapshotEntity(&e, entity) {
				entries = append(entries, e)
			}
			return true
		})
		if len(entries) > 0 {
			snaps = append(snaps, dbSnapshot{dbIndex: db, entries: entries})
		}
	}
	return snaps
}

// snapshotEntity populates the entry's type and value fields from the entity.
// Returns false if the entity type is unrecognized (entry is skipped).
func snapshotEntity(e *rdb.Entry, entity *database.DataEntity) bool {
	switch obj := entity.Data.(type) {
	case *object.Robj:
		return snapshotRobj(e, obj)
	case []byte:
		// Legacy raw string
		e.Type = rdb.EntryType(0)
		e.StringVal = append([]byte(nil), obj...)
		return true
	case List.List:
		// Legacy quicklist
		e.Type = rdb.EntryType(1)
		vals := make([][]byte, 0, obj.Len())
		obj.ForEach(func(_ int, v interface{}) bool {
			b, _ := v.([]byte)
			vals = append(vals, append([]byte(nil), b...))
			return true
		})
		e.ListVal = vals
		return true
	case *set.Set:
		// Legacy set
		e.Type = rdb.EntryType(2)
		vals := make([][]byte, 0, obj.Len())
		obj.ForEach(func(m string) bool {
			vals = append(vals, []byte(m))
			return true
		})
		e.SetVal = vals
		return true
	case dict.Dict:
		// Legacy hash
		e.Type = rdb.EntryType(4)
		h := make(map[string][]byte, obj.Len())
		obj.ForEach(func(field string, v interface{}) bool {
			b, _ := v.([]byte)
			h[field] = append([]byte(nil), b...)
			return true
		})
		e.HashVal = h
		return true
	case *SortedSet.SortedSet:
		// Legacy sorted set
		e.Type = rdb.EntryType(3)
		members := make([]rdb.ZSetMember, 0, obj.Len())
		obj.ForEachByRank(0, obj.Len(), false, func(el *SortedSet.Element) bool {
			members = append(members, rdb.ZSetMember{Member: []byte(el.Member), Score: el.Score})
			return true
		})
		e.ZSetVal = members
		return true
	default:
		return false
	}
}

// snapshotRobj handles the *object.Robj path (the primary data path).
func snapshotRobj(e *rdb.Entry, robj *object.Robj) bool {
	switch robj.Type {
	case object.TypeString:
		e.Type = rdb.EntryType(0)
		e.StringVal = append([]byte(nil), robj.GetStringBytes()...)
		return true
	case object.TypeList:
		e.Type = rdb.EntryType(1)
		list, ok := robj.Ptr.(*object.List)
		if !ok {
			return false
		}
		vals := make([][]byte, 0, list.Len())
		list.ForEach(func(_ int, val interface{}) bool {
			switch v := val.(type) {
			case []byte:
				vals = append(vals, append([]byte(nil), v...))
			case string:
				vals = append(vals, []byte(v))
			default:
				vals = append(vals, nil)
			}
			return true
		})
		e.ListVal = vals
		return true
	case object.TypeSet:
		e.Type = rdb.EntryType(2)
		s, ok := robj.Ptr.(*object.Set)
		if !ok {
			return false
		}
		vals := make([][]byte, 0, s.Len())
		s.ForEach(func(m string) bool {
			vals = append(vals, []byte(m))
			return true
		})
		e.SetVal = vals
		return true
	case object.TypeHash:
		e.Type = rdb.EntryType(4)
		h, ok := robj.Ptr.(*object.Hash)
		if !ok {
			return false
		}
		hash := make(map[string][]byte, h.Len())
		h.ForEach(func(field string, value interface{}) bool {
			switch v := value.(type) {
			case []byte:
				hash[field] = append([]byte(nil), v...)
			case string:
				hash[field] = []byte(v)
			default:
				hash[field] = nil
			}
			return true
		})
		e.HashVal = hash
		return true
	case object.TypeZSet:
		e.Type = rdb.EntryType(3)
		z, ok := robj.Ptr.(*object.ZSet)
		if !ok {
			return false
		}
		members := make([]rdb.ZSetMember, 0, z.Len())
		z.ForEach(func(member string, score float64) bool {
			members = append(members, rdb.ZSetMember{Member: []byte(member), Score: score})
			return true
		})
		e.ZSetVal = members
		return true
	default:
		return false
	}
}

// writeSnapshotRDB encodes a detached snapshot to w as a faithful RDB v11 file.
func writeSnapshotRDB(snaps []dbSnapshot, dbCount int, w io.Writer) error {
	enc := rdb.NewEncoder(w)
	if err := enc.WriteHeader(); err != nil {
		return err
	}
	if err := enc.WriteAux("redis-ver", "8.0.0"); err != nil {
		return err
	}
	if err := enc.WriteAux("redis-bits", "64"); err != nil {
		return err
	}
	for _, snap := range snaps {
		if err := enc.WriteSelectDB(snap.dbIndex); err != nil {
			return err
		}
		var ttl uint64
		for _, e := range snap.entries {
			if e.ExpireMS != 0 {
				ttl++
			}
		}
		if err := enc.WriteResizeDB(uint64(len(snap.entries)), ttl); err != nil {
			return err
		}
		for _, e := range snap.entries {
			if err := writeSnapshotEntry(enc, e); err != nil {
				return err
			}
		}
	}
	return enc.WriteEnd()
}

func writeSnapshotEntry(enc *rdb.Encoder, e rdb.Entry) error {
	switch e.Type {
	case rdb.EntryType(0):
		return enc.WriteStringEntry(e.Key, e.StringVal, e.ExpireMS)
	case rdb.EntryType(1):
		return enc.WriteListEntry(e.Key, e.ListVal, e.ExpireMS)
	case rdb.EntryType(2):
		return enc.WriteSetEntry(e.Key, e.SetVal, e.ExpireMS)
	case rdb.EntryType(4):
		return enc.WriteHashEntry(e.Key, e.HashVal, e.ExpireMS)
	case rdb.EntryType(3):
		return enc.WriteZSetEntry(e.Key, e.ZSetVal, e.ExpireMS)
	}
	return nil
}

// saveSnapshotToFile encodes a detached snapshot to rdbFilename atomically.
func (db *Server) saveSnapshotToFile(snap []dbSnapshot, dbCount int, rdbFilename string) error {
	tmp, err := os.CreateTemp(config.GetTmpDir(), "*.rdb")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := writeSnapshotRDB(snap, dbCount, tmp); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, rdbFilename)
}
