package database

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestPFAddCount(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("PFADD", "hll", "a", "b", "c"))
	ret := s.Exec(conn, utils.ToCmdLine("PFCOUNT", "hll"))
	// PFCOUNT should return approximately 3
	raw := string(ret.ToBytes())
	if raw[0] != ':' {
		t.Fatalf("PFCOUNT = %q", raw)
	}
	// Check TYPE is string
	ret2 := s.Exec(conn, utils.ToCmdLine("TYPE", "hll"))
	if string(ret2.ToBytes()) != "+string\r\n" {
		t.Fatalf("HLL TYPE should be string, got %q", ret2.ToBytes())
	}
}

func TestPFMerge(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("PFADD", "a", "x", "y"))
	s.Exec(conn, utils.ToCmdLine("PFADD", "b", "y", "z"))
	s.Exec(conn, utils.ToCmdLine("PFMERGE", "dest", "a", "b"))
	ret := s.Exec(conn, utils.ToCmdLine("PFCOUNT", "dest"))
	raw := string(ret.ToBytes())
	if raw[0] != ':' {
		t.Fatalf("PFMERGE PFCOUNT = %q", raw)
	}
}

func TestPFAddNoArgs(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("FLUSHDB"))
	// PFADD with no elements creates the key
	ret := s.Exec(conn, utils.ToCmdLine("PFADD", "hll"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("PFADD no args (create) = %q", ret.ToBytes())
	}
	// PFADD with no elements on existing key returns 0
	ret = s.Exec(conn, utils.ToCmdLine("PFADD", "hll"))
	if string(ret.ToBytes()) != ":0\r\n" {
		t.Fatalf("PFADD no args (exists) = %q", ret.ToBytes())
	}
}
