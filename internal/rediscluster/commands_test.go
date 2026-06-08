package rediscluster

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2"
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

func TestClusterFailoverTakeover(t *testing.T) {
	cc := newTestCommands(t)
	masterID := genNodeID()
	peer := newNode(masterID, "127.0.0.1", 7001)
	peer.addSlot(0)
	cc.state.mu.Lock()
	cc.state.nodes[masterID] = peer
	cc.state.slots[0] = peer
	cc.state.self.flags &^= flagMaster
	cc.state.self.flags |= flagSlave
	cc.state.self.masterID = masterID
	cc.state.mu.Unlock()

	r := enc(t, cc.handle(cmd("CLUSTER", "FAILOVER", "TAKEOVER")))
	if string(r) != "+OK\r\n" {
		t.Fatalf("CLUSTER FAILOVER TAKEOVER = %q", r)
	}

	cc.state.mu.RLock()
	isMaster := cc.state.self.flags&flagMaster != 0
	ownsSlot0 := cc.state.slots[0] == cc.state.self
	cc.state.mu.RUnlock()

	if !isMaster {
		t.Fatal("TAKEOVER should promote self to master")
	}
	if !ownsSlot0 {
		t.Fatal("TAKEOVER should claim master's slots")
	}
}

func TestClusterFailoverNotReplica(t *testing.T) {
	cc := newTestCommands(t)
	r := enc(t, cc.handle(cmd("CLUSTER", "FAILOVER")))
	if !bytes.Contains(r, []byte("You should send CLUSTER FAILOVER to a replica")) {
		t.Fatalf("FAILOVER on master should error, got %q", r)
	}
}

func TestClusterBumpEpoch(t *testing.T) {
	cc := newTestCommands(t)
	r := enc(t, cc.handle(cmd("CLUSTER", "BUMPEPOCH")))
	s := string(r)
	if !strings.HasPrefix(s, "+BUMPED ") {
		t.Fatalf("BUMPEPOCH = %q, want +BUMPED <epoch>", s)
	}
}

func TestClusterLinks(t *testing.T) {
	cc := newTestCommands(t)
	peer := newNode(genNodeID(), "127.0.0.1", 7001)
	cc.state.mu.Lock()
	cc.state.nodes[peer.id] = peer
	cc.state.mu.Unlock()

	r := enc(t, cc.handle(cmd("CLUSTER", "LINKS")))
	if r[0] != '*' {
		t.Fatalf("LINKS should return array, got %q", r)
	}
}

func TestClusterCountFailureReports(t *testing.T) {
	cc := newTestCommands(t)
	peer := newNode(genNodeID(), "127.0.0.1", 7001)
	cc.state.mu.Lock()
	cc.state.nodes[peer.id] = peer
	cc.state.failureReports.addReport(peer.id, "reporter1")
	cc.state.mu.Unlock()

	r := enc(t, cc.handle(cmd("CLUSTER", "COUNT-FAILURE-REPORTS", peer.id)))
	if string(r) != ":1\r\n" {
		t.Fatalf("COUNT-FAILURE-REPORTS = %q, want :1", r)
	}

	// Unknown node
	r = enc(t, cc.handle(cmd("CLUSTER", "COUNT-FAILURE-REPORTS", "nonexistent")))
	if r[0] != '-' {
		t.Fatalf("COUNT-FAILURE-REPORTS for unknown node should error, got %q", r)
	}
}
