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

func TestGossipMessageTypesRoundTrip(t *testing.T) {
	for _, mt := range []uint16{msgTypeFail, msgTypeFailoverAuthRequest, msgTypeFailoverAuthAck, msgTypePublishShard} {
		hdr := clusterMsgHeader{msgType: mt, currentEpoch: 7}
		encoded := encodeHeader(&hdr)
		got, err := decodeHeader(encoded)
		if err != nil {
			t.Fatalf("roundtrip mt=%d: decode error %v", mt, err)
		}
		if got.msgType != mt {
			t.Fatalf("roundtrip mt=%d: got msgType=%d", mt, got.msgType)
		}
		if got.currentEpoch != 7 {
			t.Fatalf("roundtrip mt=%d: got currentEpoch=%d, want 7", mt, got.currentEpoch)
		}
	}
}

func TestGossipPFailPropagatedAsReport(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	stA := newClusterState("127.0.0.1", 7000, filepath.Join(dirA, "nodes.conf"))
	stB := newClusterState("127.0.0.1", 7001, filepath.Join(dirB, "nodes.conf"))

	// Make A know about B and vice versa
	stA.mu.Lock()
	stA.nodes[stB.self.id] = &clusterNode{id: stB.self.id, ip: "127.0.0.1", port: 7001, cport: 17001, flags: flagMaster, linkUp: true}
	stA.mu.Unlock()
	stB.mu.Lock()
	stB.nodes[stA.self.id] = &clusterNode{id: stA.self.id, ip: "127.0.0.1", port: 7000, cport: 17000, flags: flagMaster, linkUp: true}
	stB.mu.Unlock()

	// Simulate A receiving a gossip from B that reports a third node C as PFAIL
	nodeCID := genNodeID()
	stA.mu.Lock()
	nodeC := &clusterNode{id: nodeCID, ip: "127.0.0.1", port: 7002, cport: 17002, flags: flagMaster, linkUp: true}
	stA.nodes[nodeCID] = nodeC
	stA.mu.Unlock()

	// Build a message from B that includes C with PFAIL flag
	bus := newGossipBus(stA)
	hdr := &clusterMsgHeader{
		msgType:      msgTypePing,
		currentEpoch: 1,
		configEpoch:  1,
		port:         7001,
		cport:        17001,
		senderID:     stB.self.id,
	}
	entries := []gossipEntry{
		{id: nodeCID, ip: "127.0.0.1", port: 7002, cport: 17002, flags: uint32(flagPFail)},
	}

	bus.mergeFromMessage(hdr, entries, "127.0.0.1")

	// Verify: A should have recorded a PFAIL report from B for C
	stA.mu.RLock()
	fr := stA.failureReports
	stA.mu.RUnlock()

	if fr.count(nodeCID) == 0 {
		t.Fatal("expected PFAIL failure report for nodeC from senderB")
	}

	// Verify: C should have PFAIL flag set
	stA.mu.RLock()
	hasPFail := nodeC.flags&flagPFail != 0
	stA.mu.RUnlock()
	if !hasPFail {
		t.Fatal("nodeC should have PFAIL flag set after gossip")
	}
}

func TestGossipFailAdoptedFromGossip(t *testing.T) {
	dir := t.TempDir()
	st := newClusterState("127.0.0.1", 7000, filepath.Join(dir, "nodes.conf"))

	nodeBID := genNodeID()
	nodeCID := genNodeID()
	st.mu.Lock()
	nodeB := &clusterNode{id: nodeBID, ip: "127.0.0.1", port: 7001, cport: 17001, flags: flagMaster, linkUp: true}
	nodeC := &clusterNode{id: nodeCID, ip: "127.0.0.1", port: 7002, cport: 17002, flags: flagMaster, linkUp: true}
	st.nodes[nodeBID] = nodeB
	st.nodes[nodeCID] = nodeC
	st.mu.Unlock()

	bus := newGossipBus(st)
	hdr := &clusterMsgHeader{
		msgType:      msgTypeFail,
		currentEpoch: 1,
		configEpoch:  1,
		port:         7001,
		cport:        17001,
		senderID:     nodeBID,
	}
	entries := []gossipEntry{
		{id: nodeCID, ip: "127.0.0.1", port: 7002, cport: 17002, flags: uint32(flagFail)},
	}

	bus.mergeFromMessage(hdr, entries, "127.0.0.1")

	st.mu.RLock()
	hasFail := nodeC.flags&flagFail != 0
	hasPFail := nodeC.flags&flagPFail != 0
	st.mu.RUnlock()

	if !hasFail {
		t.Fatal("nodeC should have FAIL flag after receiving FAIL gossip")
	}
	if hasPFail {
		t.Fatal("nodeC should NOT have PFAIL flag once FAIL is set")
	}
}
