package aof

import (
	"bytes"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	List "github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/datastruct/set"
	SortedSet "github.com/amemiya02/hayakv/internal/datastruct/sortedset"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/persist/rdb"
)

// fakeEngine is a minimal database.DBEngine for bridge tests: it stores one db.
type fakeEngine struct {
	entries map[string]*database.DataEntity
	ttl     map[string]time.Time
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{entries: map[string]*database.DataEntity{}, ttl: map[string]time.Time{}}
}

func (f *fakeEngine) ForEach(dbIndex int, cb func(key string, data *database.DataEntity, expiration *time.Time) bool) {
	if dbIndex != 0 {
		return
	}
	for k, v := range f.entries {
		var exp *time.Time
		if t, ok := f.ttl[k]; ok {
			tt := t
			exp = &tt
		}
		if !cb(k, v, exp) {
			return
		}
	}
}
func (f *fakeEngine) GetDBSize(dbIndex int) (int, int) {
	if dbIndex != 0 {
		return 0, 0
	}
	return len(f.entries), len(f.ttl)
}

func TestDumpEngineToRDB(t *testing.T) {
	eng := newFakeEngine()
	eng.entries["s"] = &database.DataEntity{Data: []byte("hello")}
	ql := List.NewQuickList()
	ql.Add([]byte("a"))
	ql.Add([]byte("b"))
	eng.entries["l"] = &database.DataEntity{Data: ql}
	st := set.Make("x", "y")
	eng.entries["se"] = &database.DataEntity{Data: st}
	h := dict.MakeSimple()
	h.Put("f", []byte("1"))
	eng.entries["h"] = &database.DataEntity{Data: h}
	zs := SortedSet.Make()
	zs.Add("m", 1.5)
	eng.entries["z"] = &database.DataEntity{Data: zs}

	var buf bytes.Buffer
	if err := DumpEngineToRDB(eng, 1, &buf); err != nil {
		t.Fatalf("DumpEngineToRDB: %v", err)
	}

	dec := rdb.NewDecoder(bytes.NewReader(buf.Bytes()))
	got := map[string]rdb.Entry{}
	err := dec.Parse(func(e rdb.Entry) bool {
		got[string(e.Key)] = e
		return true
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d entries, want 5", len(got))
	}
	if !bytes.Equal(got["s"].StringVal, []byte("hello")) {
		t.Fatalf("string mismatch: %+v", got["s"])
	}
	if len(got["l"].ListVal) != 2 {
		t.Fatalf("list mismatch: %+v", got["l"])
	}
	if len(got["se"].SetVal) != 2 {
		t.Fatalf("set mismatch: %+v", got["se"])
	}
	if !bytes.Equal(got["h"].HashVal["f"], []byte("1")) {
		t.Fatalf("hash mismatch: %+v", got["h"])
	}
	if got["z"].ZSetVal[0].Score != 1.5 {
		t.Fatalf("zset mismatch: %+v", got["z"])
	}
}
