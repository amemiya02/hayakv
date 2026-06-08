package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/datastruct/set"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestEstimateEntitySizeStableForFixedInput(t *testing.T) {
	e := &database.DataEntity{Data: []byte("hello")}
	a := estimateEntitySize("k", e)
	b := estimateEntitySize("k", e)
	if a != b {
		t.Fatalf("size not stable: %d vs %d", a, b)
	}
	if a == 0 {
		t.Fatalf("size of non-empty string entity is 0")
	}
}

func TestEstimateEntitySizeGrowsWithValue(t *testing.T) {
	small := estimateEntitySize("k", &database.DataEntity{Data: []byte("v")})
	big := estimateEntitySize("k", &database.DataEntity{Data: []byte("0123456789ABCDEF0123456789ABCDEF")})
	if big <= small {
		t.Fatalf("bigger value should estimate larger: small=%d big=%d", small, big)
	}
}

func TestEstimateEntitySizeSet(t *testing.T) {
	s := set.Make("a", "bb", "ccc")
	sz := estimateEntitySize("k", &database.DataEntity{Data: s})
	if sz == 0 {
		t.Fatalf("set entity size is 0")
	}
}

func TestUsedMemorySumsDBs(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	before := s.usedMemory()
	db := s.mustSelectDB(0)
	db.PutEntity("a", &database.DataEntity{Data: []byte("0123456789")})
	after := s.usedMemory()
	if after <= before {
		t.Fatalf("usedMemory did not grow: before=%d after=%d", before, after)
	}
}

func TestMemoryStats(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "k", "v"))
	r := s.Exec(conn, utils.ToCmdLine("MEMORY", "STATS"))
	body := string(r.ToBytes())
	for _, field := range []string{"keys.count", "dataset.bytes", "peak.allocated", "total.allocated"} {
		if !strings.Contains(body, field) {
			t.Fatalf("MEMORY STATS missing %s: %s", field, body)
		}
	}
}

func TestMemoryDoctor(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("MEMORY", "DOCTOR"))
	if len(r.ToBytes()) == 0 {
		t.Fatal("MEMORY DOCTOR empty")
	}
}
