package rediscluster

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// recordingEngine records the last command it actually executed.
type recordingEngine struct{ last [][]byte }

func (e *recordingEngine) Exec(_ iredis.Connection, cmd iface.CmdLine) iredis.Reply {
	e.last = cmd
	return protocol.MakeOkReply()
}
func (e *recordingEngine) AfterClientClose(iredis.Connection) {}
func (e *recordingEngine) Close()                             {}

func newDecorator(t *testing.T) (*ClusterEngine, *clusterState, *recordingEngine) {
	t.Helper()
	st := newClusterState("127.0.0.1", 7000, filepath.Join(t.TempDir(), "nodes.conf"))
	inner := &recordingEngine{}
	ce := NewClusterEngine(inner, st)
	return ce, st, inner
}

func encR(t *testing.T, r iredis.Reply) []byte {
	t.Helper()
	return resp2.Codec{}.Encode(r, iredis.RESP2)
}

func TestRedirectMovedWhenNotOwner(t *testing.T) {
	ce, st, inner := newDecorator(t)
	// Give slot for "foo" (12182) to a PEER.
	peer := newNode(genNodeID(), "10.0.0.9", 7002)
	st.mu.Lock()
	st.nodes[peer.id] = peer
	st.slots[12182] = peer
	peer.addSlot(12182)
	st.mu.Unlock()
	got := encR(t, ce.Exec(nil, cmd("GET", "foo")))
	want := []byte("-MOVED 12182 10.0.0.9:7002\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("MOVED reply = %q, want %q", got, want)
	}
	if inner.last != nil {
		t.Fatal("inner engine must NOT execute a MOVED command")
	}
}

func TestRedirectLocalWhenOwner(t *testing.T) {
	ce, st, inner := newDecorator(t)
	st.addSlots([]uint16{12182}) // we own foo's slot
	got := encR(t, ce.Exec(nil, cmd("GET", "foo")))
	if string(got) != "+OK\r\n" {
		t.Fatalf("owner GET reply = %q, want +OK", got)
	}
	if inner.last == nil || string(inner.last[0]) != "GET" {
		t.Fatalf("inner engine should have executed GET, got %v", inner.last)
	}
}

func TestRedirectCrossSlot(t *testing.T) {
	ce, st, _ := newDecorator(t)
	st.addSlots([]uint16{Key2Slot("a"), Key2Slot("b")}) // own both slots
	got := encR(t, ce.Exec(nil, cmd("MSET", "a", "1", "b", "2")))
	if !bytes.Contains(got, []byte("CROSSSLOT")) {
		t.Fatalf("multi-slot MSET should CROSSSLOT, got %q", got)
	}
}

func TestRedirectKeylessPassThrough(t *testing.T) {
	ce, _, inner := newDecorator(t)
	if string(encR(t, ce.Exec(nil, cmd("PING")))) != "+OK\r\n" {
		t.Fatal("keyless command must pass through")
	}
	if inner.last == nil || string(inner.last[0]) != "PING" {
		t.Fatalf("PING should reach inner, got %v", inner.last)
	}
}

func TestRedirectClusterCommandIntercepted(t *testing.T) {
	ce, st, inner := newDecorator(t)
	got := encR(t, ce.Exec(nil, cmd("CLUSTER", "MYID")))
	want := append(append([]byte("$40\r\n"), []byte(st.myID())...), []byte("\r\n")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("CLUSTER MYID = %q, want %q", got, want)
	}
	if inner.last != nil {
		t.Fatal("CLUSTER must be handled by the decorator, not the inner engine")
	}
}
