package eventloop

import (
	"testing"
)

func TestBlockRegistryBlockAndUnblock(t *testing.T) {
	reg := newBlockRegistry()
	c1 := &client{fd: 1}
	c2 := &client{fd: 2}

	reg.block(c1, []string{"key1", "key2"})
	reg.block(c2, []string{"key1"})

	w := reg.waiters("key1")
	if len(w) != 2 {
		t.Fatalf("waiters(key1) = %d, want 2", len(w))
	}
	w = reg.waiters("key2")
	if len(w) != 1 {
		t.Fatalf("waiters(key2) = %d, want 1", len(w))
	}

	reg.unblock(c1)
	w = reg.waiters("key1")
	if len(w) != 1 || w[0] != c2 {
		t.Fatalf("after unblock c1: waiters(key1) = %v, want [c2]", w)
	}
	w = reg.waiters("key2")
	if len(w) != 0 {
		t.Fatalf("after unblock c1: waiters(key2) = %d, want 0", len(w))
	}
}

func TestBlockRegistryEmpty(t *testing.T) {
	reg := newBlockRegistry()
	w := reg.waiters("nonexistent")
	if len(w) != 0 {
		t.Fatalf("expected empty waiters, got %d", len(w))
	}
}
