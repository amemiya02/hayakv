package rediscluster

import (
	"path/filepath"
	"testing"
)

func TestStateOwnershipAndPersistence(t *testing.T) {
	confPath := filepath.Join(t.TempDir(), "nodes.conf")
	st := newClusterState("127.0.0.1", 7000, confPath)
	if len(st.myID()) != 40 {
		t.Fatalf("myID len = %d", len(st.myID()))
	}
	if st.ownerOf(100) != nil {
		t.Fatal("fresh state should own no slots")
	}
	st.addSlots([]uint16{100, 101, 102})
	if o := st.ownerOf(100); o == nil || !o.isMyself() {
		t.Fatalf("slot 100 should be owned by myself, got %v", o)
	}
	if !st.imOwner(100) || st.imOwner(200) {
		t.Fatalf("imOwner wrong")
	}
	if st.assignedSlots() != 3 {
		t.Fatalf("assignedSlots = %d, want 3", st.assignedSlots())
	}
	// persist + reload
	if err := st.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	st2 := newClusterState("127.0.0.1", 7000, confPath)
	if err := st2.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if st2.myID() != st.myID() {
		t.Fatalf("reloaded id changed: %s vs %s", st2.myID(), st.myID())
	}
	if !st2.imOwner(100) || st2.assignedSlots() != 3 {
		t.Fatalf("reloaded ownership wrong: imOwner(100)=%v assigned=%d", st2.imOwner(100), st2.assignedSlots())
	}
}
