package object

import (
	"testing"
	"time"
)

func TestHashFieldTTLEncoding(t *testing.T) {
	h := NewHash()
	h.Put("f1", "v1")
	if h.CurrentEncoding() != EncListpack {
		t.Fatalf("fresh hash should be listpack, got %v", h.CurrentEncoding())
	}

	nowMs := time.Now().UnixMilli()
	h.SetFieldExpire("f1", nowMs+1_000_000)
	if h.CurrentEncoding() != EncListpackEx {
		t.Fatalf("hash with field TTL should be listpackex, got %v", h.CurrentEncoding())
	}

	exp, ok := h.FieldExpire("f1")
	if !ok || exp != nowMs+1_000_000 {
		t.Fatalf("FieldExpire = %d,%v", exp, ok)
	}

	// PersistField
	if !h.PersistField("f1") {
		t.Fatal("PersistField should return true")
	}
	if h.CurrentEncoding() != EncListpack {
		t.Fatalf("after persist, should be listpack again, got %v", h.CurrentEncoding())
	}
}

func TestPurgeExpiredFields(t *testing.T) {
	h := NewHash()
	h.Put("f1", "v1")
	h.Put("f2", "v2")
	h.Put("f3", "v3")

	nowMs := time.Now().UnixMilli()
	h.SetFieldExpire("f1", nowMs-1000)      // already expired
	h.SetFieldExpire("f2", nowMs+1_000_000) // not expired

	expired := h.PurgeExpiredFields(nowMs)
	if len(expired) != 1 || expired[0] != "f1" {
		t.Fatalf("purge: %v", expired)
	}

	if _, ok := h.Get("f1"); ok {
		t.Fatal("f1 should be purged")
	}
	if _, ok := h.Get("f2"); !ok {
		t.Fatal("f2 should still exist")
	}
	if _, ok := h.Get("f3"); !ok {
		t.Fatal("f3 should still exist (no expiry)")
	}
}

func TestHashFieldExpiries(t *testing.T) {
	h := NewHash()
	if h.HasFieldExpiries() {
		t.Fatal("new hash should not have field expiries")
	}
	if h.FieldExpireCount() != 0 {
		t.Fatal("new hash should have 0 field expiries")
	}

	h.SetFieldExpire("f1", 1000)
	if !h.HasFieldExpiries() {
		t.Fatal("should have field expiries after set")
	}
	if h.FieldExpireCount() != 1 {
		t.Fatalf("expected 1 field expiry, got %d", h.FieldExpireCount())
	}
}
