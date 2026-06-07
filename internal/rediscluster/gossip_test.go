package rediscluster

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func TestClusterMsgHeaderRoundTrip(t *testing.T) {
	h := clusterMsgHeader{
		msgType:      msgTypePing,
		senderID:     "0123456789abcdef0123456789abcdef01234567",
		currentEpoch: 7,
		configEpoch:  3,
		port:         7000,
		cport:        17000,
		flags:        flagMaster,
	}
	for s := uint16(0); s < 10; s++ {
		h.slots[s/8] |= 1 << (s % 8)
	}
	buf := encodeHeader(&h)
	if len(buf) != headerLen {
		t.Fatalf("encoded header len = %d, want %d", len(buf), headerLen)
	}
	if !bytes.Equal(buf[:4], []byte(clusterMsgSig)) {
		t.Fatalf("signature = %q, want %q", buf[:4], clusterMsgSig)
	}
	got, err := decodeHeader(buf)
	if err != nil {
		t.Fatalf("decodeHeader: %v", err)
	}
	if got.msgType != msgTypePing || got.senderID != h.senderID ||
		got.currentEpoch != 7 || got.configEpoch != 3 ||
		got.port != 7000 || got.cport != 17000 || got.flags != flagMaster {
		t.Fatalf("decoded header mismatch: %+v", got)
	}
	if !bytes.Equal(got.slots[:], h.slots[:]) {
		t.Fatal("slot bitmap mismatch after round-trip")
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestTwoNodesMeetLearnEachOther(t *testing.T) {
	dir := t.TempDir()
	// Node A on a bus port, node B on another. Use high ports to avoid +10000 clash.
	stA := newClusterState("127.0.0.1", 18000, filepath.Join(dir, "a.conf"))
	stB := newClusterState("127.0.0.1", 18100, filepath.Join(dir, "b.conf"))
	stA.self.cport = 28000
	stB.self.cport = 28100
	stA.addSlots([]uint16{1, 2, 3})
	stB.addSlots([]uint16{100, 200})

	busA := newGossipBus(stA)
	busB := newGossipBus(stB)
	if err := busA.start(); err != nil {
		t.Fatalf("busA.start: %v", err)
	}
	defer busA.stop()
	if err := busB.start(); err != nil {
		t.Fatalf("busB.start: %v", err)
	}
	defer busB.stop()

	// A meets B.
	if err := busA.meet("127.0.0.1", 18100, 28100); err != nil {
		t.Fatalf("meet: %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		return stA.nodeByID(stB.myID()) != nil && stB.nodeByID(stA.myID()) != nil
	})
	// A should learn B owns slots 100,200; B should learn A owns 1,2,3.
	bInA := stA.nodeByID(stB.myID())
	if bInA == nil || !bInA.hasSlot(100) || !bInA.hasSlot(200) {
		t.Fatalf("A did not learn B's slots: %+v", bInA)
	}
	aInB := stB.nodeByID(stA.myID())
	if aInB == nil || !aInB.hasSlot(1) || !aInB.hasSlot(3) {
		t.Fatalf("B did not learn A's slots: %+v", aInB)
	}
}
