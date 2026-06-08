package rdb

import (
	"bytes"
	"testing"
)

func TestEncoderHeaderAndEOF(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if err := enc.WriteAux("redis-ver", "8.0.0"); err != nil {
		t.Fatalf("WriteAux: %v", err)
	}
	if err := enc.WriteSelectDB(0); err != nil {
		t.Fatalf("WriteSelectDB: %v", err)
	}
	if err := enc.WriteResizeDB(1, 0); err != nil {
		t.Fatalf("WriteResizeDB: %v", err)
	}
	if err := enc.WriteStringEntry([]byte("k"), []byte("v"), 0); err != nil {
		t.Fatalf("WriteStringEntry: %v", err)
	}
	if err := enc.WriteEnd(); err != nil {
		t.Fatalf("WriteEnd: %v", err)
	}
	b := buf.Bytes()
	if !bytes.HasPrefix(b, []byte("REDIS0012")) {
		t.Fatalf("missing header, got %q", b[:9])
	}
	// last 9 bytes = 0xFF + 8-byte crc; the byte before the crc trailer is the EOF opcode.
	if b[len(b)-9] != opEOF {
		t.Fatalf("missing EOF opcode before crc, got %#x", b[len(b)-9])
	}
}

func TestEncoderExpireOpcode(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	_ = enc.WriteHeader()
	_ = enc.WriteSelectDB(0)
	if err := enc.WriteStringEntry([]byte("k"), []byte("v"), 1700000000000); err != nil {
		t.Fatalf("WriteStringEntry: %v", err)
	}
	b := buf.Bytes()
	if !bytes.Contains(b, []byte{opExpireMS}) {
		t.Fatalf("expected EXPIRETIME_MS opcode in stream")
	}
}
