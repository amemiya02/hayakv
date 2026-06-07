package rediscluster

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/amemiya02/hayakv/internal/proto/resp2"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
)

func enc(t *testing.T, r iredis.Reply) []byte {
	t.Helper()
	return resp2.Codec{}.Encode(r, iredis.RESP2)
}

func newTestCommands(t *testing.T) *clusterCommands {
	t.Helper()
	st := newClusterState("127.0.0.1", 7000, filepath.Join(t.TempDir(), "nodes.conf"))
	// no real keyspace in unit tests: return zero keys.
	return newClusterCommands(st, func(slot uint16, count int) []string { return nil })
}

func TestClusterKeyslot(t *testing.T) {
	cc := newTestCommands(t)
	got := enc(t, cc.handle(cmd("CLUSTER", "KEYSLOT", "foo")))
	if !bytes.Equal(got, []byte(":12182\r\n")) {
		t.Fatalf("KEYSLOT foo = %q, want :12182", got)
	}
}

func TestClusterMyID(t *testing.T) {
	cc := newTestCommands(t)
	got := enc(t, cc.handle(cmd("CLUSTER", "MYID")))
	want := append(append([]byte("$40\r\n"), []byte(cc.state.myID())...), []byte("\r\n")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("MYID = %q, want %q", got, want)
	}
}

func TestClusterInfoFailWhenNoSlots(t *testing.T) {
	cc := newTestCommands(t)
	got := string(enc(t, cc.handle(cmd("CLUSTER", "INFO"))))
	if !bytes.Contains([]byte(got), []byte("cluster_enabled:1")) ||
		!bytes.Contains([]byte(got), []byte("cluster_state:fail")) ||
		!bytes.Contains([]byte(got), []byte("cluster_slots_assigned:0")) {
		t.Fatalf("INFO body wrong:\n%s", got)
	}
}

func TestClusterCountKeysInSlotZero(t *testing.T) {
	cc := newTestCommands(t)
	got := enc(t, cc.handle(cmd("CLUSTER", "COUNTKEYSINSLOT", "12182")))
	if !bytes.Equal(got, []byte(":0\r\n")) {
		t.Fatalf("COUNTKEYSINSLOT = %q, want :0", got)
	}
}

func TestClusterAddDelSlots(t *testing.T) {
	cc := newTestCommands(t)
	if r := enc(t, cc.handle(cmd("CLUSTER", "ADDSLOTS", "0", "1", "2"))); string(r) != "+OK\r\n" {
		t.Fatalf("ADDSLOTS = %q", r)
	}
	if !cc.state.imOwner(1) {
		t.Fatal("slot 1 not owned after ADDSLOTS")
	}
	// double-assign must error
	if r := enc(t, cc.handle(cmd("CLUSTER", "ADDSLOTS", "1"))); string(r[0]) != "-"[0:1] {
		t.Fatalf("re-ADDSLOTS should error, got %q", r)
	}
	if r := enc(t, cc.handle(cmd("CLUSTER", "DELSLOTS", "1"))); string(r) != "+OK\r\n" {
		t.Fatalf("DELSLOTS = %q", r)
	}
	if cc.state.imOwner(1) {
		t.Fatal("slot 1 still owned after DELSLOTS")
	}
}

func TestClusterAddSlotsRange(t *testing.T) {
	cc := newTestCommands(t)
	if r := enc(t, cc.handle(cmd("CLUSTER", "ADDSLOTSRANGE", "0", "100", "200", "300"))); string(r) != "+OK\r\n" {
		t.Fatalf("ADDSLOTSRANGE = %q", r)
	}
	if cc.state.assignedSlots() != 202 { // 0..100 (101) + 200..300 (101)
		t.Fatalf("assigned = %d, want 202", cc.state.assignedSlots())
	}
}

func TestClusterSetSlotNode(t *testing.T) {
	cc := newTestCommands(t)
	// Introduce a peer and give it a slot via SETSLOT ... NODE.
	peer := newNode(genNodeID(), "127.0.0.1", 7001)
	cc.state.mu.Lock()
	cc.state.nodes[peer.id] = peer
	cc.state.mu.Unlock()
	if r := enc(t, cc.handle(cmd("CLUSTER", "SETSLOT", "42", "NODE", peer.id))); string(r) != "+OK\r\n" {
		t.Fatalf("SETSLOT NODE = %q", r)
	}
	if o := cc.state.ownerOf(42); o == nil || o.id != peer.id {
		t.Fatalf("slot 42 owner = %v, want peer", o)
	}
}

// cmd is a tiny helper to build a [][]byte command line.
func cmd(parts ...string) [][]byte {
	out := make([][]byte, len(parts))
	for i, p := range parts {
		out[i] = []byte(p)
	}
	return out
}
