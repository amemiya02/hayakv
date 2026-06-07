package rediscluster

import (
	"strings"
	"testing"
)

func TestGenNodeID(t *testing.T) {
	id := genNodeID()
	if len(id) != 40 {
		t.Fatalf("node id len = %d, want 40", len(id))
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("node id has non-hex rune %q", r)
		}
	}
	if genNodeID() == id {
		t.Fatal("node ids should be random")
	}
}

func TestNodeSlotBitmap(t *testing.T) {
	n := newNode(genNodeID(), "127.0.0.1", 7000)
	if n.cport != 17000 {
		t.Fatalf("cport = %d, want 17000", n.cport)
	}
	n.addSlot(0)
	n.addSlot(1)
	n.addSlot(16383)
	if !n.hasSlot(0) || !n.hasSlot(16383) || n.hasSlot(2) {
		t.Fatalf("bitmap wrong: 0=%v 16383=%v 2=%v", n.hasSlot(0), n.hasSlot(16383), n.hasSlot(2))
	}
	if n.slotCount() != 3 {
		t.Fatalf("slotCount = %d, want 3", n.slotCount())
	}
	// ranges should coalesce 0-1 and 16383-16383.
	rs := n.slotRanges()
	if len(rs) != 2 || rs[0][0] != 0 || rs[0][1] != 1 || rs[1][0] != 16383 || rs[1][1] != 16383 {
		t.Fatalf("slotRanges = %v", rs)
	}
}
