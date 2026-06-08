package database

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestBitOpAnd(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff\xff"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x0f\x0f"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "AND", "dest", "a", "b"))
	// AND of 0xffff and 0x0f0f = 0x0f0f = 2 bytes
	if string(ret.ToBytes()) != ":2\r\n" {
		t.Fatalf("expected :2, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$2\r\n\x0f\x0f\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpOr(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff\x00"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x00\xff"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "OR", "dest", "a", "b"))
	if string(ret.ToBytes()) != ":2\r\n" {
		t.Fatalf("expected :2, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$2\r\n\xff\xff\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpXor(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff\xff"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x0f\x0f"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "XOR", "dest", "a", "b"))
	if string(ret.ToBytes()) != ":2\r\n" {
		t.Fatalf("expected :2, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$2\r\n\xf0\xf0\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpNot(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff\x00"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "NOT", "dest", "a"))
	if string(ret.ToBytes()) != ":2\r\n" {
		t.Fatalf("expected :2, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$2\r\n\x00\xff\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpDiff(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	// a = 0xFF 0xFF, b = 0x0F 0xFF
	// DIFF = a AND NOT(b) = 0xFF AND NOT(0x0F 0xFF) = 0xFF AND 0xF0 0x00 = 0xF0 0x00
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff\xff"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x0f\xff"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "DIFF", "dest", "a", "b"))
	if string(ret.ToBytes()) != ":2\r\n" {
		t.Fatalf("expected :2, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$2\r\n\xf0\x00\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpDiff1(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	// a = 0x0F 0x00, b = 0xF0 0xFF
	// DIFF1 = NOT(a) AND b = NOT(0x0F 0x00) AND 0xF0 0xFF = 0xF0 0xFF AND 0xF0 0xFF = 0xF0 0xFF
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\x0f\x00"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\xf0\xff"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "DIFF1", "dest", "a", "b"))
	if string(ret.ToBytes()) != ":2\r\n" {
		t.Fatalf("expected :2, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$2\r\n\xf0\xff\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpAndOr(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	// a = 0xFF 0x00, b = 0x0F 0x00, c = 0x00 0xFF
	// ANDOR = a AND (b OR c) = 0xFF 0x00 AND (0x0F 0x00 OR 0x00 0xFF) = 0xFF 0x00 AND 0x0F 0xFF = 0x0F 0x00
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff\x00"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x0f\x00"))
	s.Exec(conn, utils.ToCmdLine("SET", "c", "\x00\xff"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "ANDOR", "dest", "a", "b", "c"))
	if string(ret.ToBytes()) != ":2\r\n" {
		t.Fatalf("expected :2, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$2\r\n\x0f\x00\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpOne(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	// a = 0x0F (bits 0-3 set), b = 0xF0 (bits 4-7 set)
	// ONE = bits set in exactly one source = 0xFF (all bits unique to one source)
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\x0f"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\xf0"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "ONE", "dest", "a", "b"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("expected :1, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$1\r\n\xff\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpOneOverlap(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	// a = 0x0F, b = 0x0F — bits 0-3 set in both, so no bits are in exactly one source
	// ONE = 0x00
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\x0f"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x0f"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "ONE", "dest", "a", "b"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("expected :1, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$1\r\n\x00\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpNotRequiresOneKey(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x00"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "NOT", "dest", "a", "b"))
	raw := string(ret.ToBytes())
	if raw[:4] != "-ERR" {
		t.Fatalf("expected error for NOT with 2 keys, got %s", raw)
	}
}

func TestBitOpInvalidOp(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "INVALID", "dest", "a"))
	raw := string(ret.ToBytes())
	if raw[:4] != "-ERR" {
		t.Fatalf("expected error for invalid op, got %s", raw)
	}
}

func TestBitOpMissingArgs(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "AND"))
	raw := string(ret.ToBytes())
	if raw[:4] != "-ERR" {
		t.Fatalf("expected error for missing args, got %s", raw)
	}
}

func TestBitOpNonexistentKey(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff"))
	// Nonexistent key should be treated as empty (0x00 for OR)
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "OR", "dest", "a", "nonexistent"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("expected :1, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$1\r\n\xff\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}

func TestBitOpDifferentLengths(t *testing.T) {
	s := NewStandaloneServer()
	conn := connection.NewFakeConn()
	// a = 3 bytes, b = 1 byte
	// OR should produce 3 bytes, with b padded with 0x00
	s.Exec(conn, utils.ToCmdLine("SET", "a", "\xff\xff\xff"))
	s.Exec(conn, utils.ToCmdLine("SET", "b", "\x0f"))
	ret := s.Exec(conn, utils.ToCmdLine("BITOP", "OR", "dest", "a", "b"))
	if string(ret.ToBytes()) != ":3\r\n" {
		t.Fatalf("expected :3, got %s", ret.ToBytes())
	}
	val := s.Exec(conn, utils.ToCmdLine("GET", "dest"))
	expected := "$3\r\n\xff\xff\xff\r\n"
	if string(val.ToBytes()) != expected {
		t.Fatalf("unexpected dest value: %q, expected %q", val.ToBytes(), expected)
	}
}
