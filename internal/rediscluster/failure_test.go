package rediscluster

import "testing"

func TestFailureReports(t *testing.T) {
	fr := newFailureReports(3) // quorum = 3
	fr.addReport("nodeX", "reporterA")
	fr.addReport("nodeX", "reporterB")
	if fr.count("nodeX") != 2 {
		t.Fatalf("count = %d", fr.count("nodeX"))
	}
	if fr.hasQuorum("nodeX") {
		t.Fatal("2 reports should not reach quorum 3")
	}
	fr.addReport("nodeX", "reporterC")
	if !fr.hasQuorum("nodeX") {
		t.Fatal("3 reports should reach quorum")
	}
}

func TestFailureReportsCleanup(t *testing.T) {
	fr := newFailureReports(1)
	fr.addReport("nodeX", "reporterA")
	if !fr.hasQuorum("nodeX") {
		t.Fatal("1 report should reach quorum 1")
	}
	// Manually set a stale timestamp
	fr.m["nodeX"]["reporterA"] = 1 // very old
	fr.cleanup("nodeX", 15000)
	if fr.count("nodeX") != 0 {
		t.Fatalf("after cleanup, count should be 0, got %d", fr.count("nodeX"))
	}
}

func TestMarkPFailIfTimedOut(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")
	peer := newNode(genNodeID(), "127.0.0.1", 7001)
	s.mu.Lock()
	s.nodes[peer.id] = peer
	s.mu.Unlock()

	// Set pongRecv to a very old time
	s.mu.Lock()
	peer.pongRecv = 1 // very old
	s.mu.Unlock()

	s.markPFailIfTimedOut(15000) // 15 second timeout

	s.mu.RLock()
	isPFail := peer.flags&flagPFail != 0
	s.mu.RUnlock()

	if !isPFail {
		t.Fatal("peer should be marked PFAIL after timeout")
	}
}

func TestMarkNodeFail(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")
	peer := newNode(genNodeID(), "127.0.0.1", 7001)
	s.mu.Lock()
	s.nodes[peer.id] = peer
	peer.flags |= flagPFail
	s.failureReports.addReport(peer.id, "reporterA")
	s.failureReports.addReport(peer.id, "reporterB")
	s.failureReports.quorum = 2
	s.mu.Unlock()

	if !s.markNodeFail(peer.id) {
		t.Fatal("should mark node FAIL with quorum")
	}

	s.mu.RLock()
	isFail := peer.flags&flagFail != 0
	isPFail := peer.flags&flagPFail != 0
	s.mu.RUnlock()

	if !isFail {
		t.Fatal("peer should have flagFail set")
	}
	if isPFail {
		t.Fatal("peer should no longer have flagPFail after being marked FAIL")
	}
}
