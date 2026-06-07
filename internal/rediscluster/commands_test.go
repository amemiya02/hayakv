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

// cmd is a tiny helper to build a [][]byte command line.
func cmd(parts ...string) [][]byte {
	out := make([][]byte, len(parts))
	for i, p := range parts {
		out[i] = []byte(p)
	}
	return out
}
