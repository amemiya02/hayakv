package rediscluster

import "testing"

func TestFailureReports(t *testing.T) {
	fr := newFailureReports()
	// All reporters are masters so they count toward quorum.
	nodes := map[string]*clusterNode{
		"reporterA": {id: "reporterA", flags: flagMaster},
		"reporterB": {id: "reporterB", flags: flagMaster},
		"reporterC": {id: "reporterC", flags: flagMaster},
	}
	fr.addReport("nodeX", "reporterA")
	fr.addReport("nodeX", "reporterB")
	if fr.count("nodeX") != 2 {
		t.Fatalf("count = %d", fr.count("nodeX"))
	}
	// 2 reports, quorum = 5/2+1 = 3 → not enough
	if fr.hasQuorum("nodeX", 5, nodes) {
		t.Fatal("2 reports should not reach quorum (5 masters, need 3)")
	}
	fr.addReport("nodeX", "reporterC")
	// 3 reports, quorum = 5/2+1 = 3 → enough
	if !fr.hasQuorum("nodeX", 5, nodes) {
		t.Fatal("3 reports should reach quorum")
	}
	// 3 reports, quorum = 7/2+1 = 4 → not enough
	if fr.hasQuorum("nodeX", 7, nodes) {
		t.Fatal("3 reports should not reach quorum for 7 masters")
	}
}

func TestFailureReportsOnlyCountsMasters(t *testing.T) {
	fr := newFailureReports()
	// reporterA is a master, reporterB is a replica
	nodes := map[string]*clusterNode{
		"reporterA": {id: "reporterA", flags: flagMaster},
		"reporterB": {id: "reporterB", flags: flagSlave},
	}
	fr.addReport("nodeX", "reporterA")
	fr.addReport("nodeX", "reporterB")
	// 2 raw reports, but only 1 master reporter → quorum = 3/2+1 = 2 → not enough
	if fr.hasQuorum("nodeX", 3, nodes) {
		t.Fatal("replica reporters should not count toward quorum")
	}
	// With 1 master reporter, quorum = 1/2+1 = 1 → enough
	if !fr.hasQuorum("nodeX", 1, nodes) {
		t.Fatal("1 master reporter should reach quorum for 1 master")
	}
}

func TestFailureReportsCleanup(t *testing.T) {
	fr := newFailureReports()
	nodes := map[string]*clusterNode{
		"reporterA": {id: "reporterA", flags: flagMaster},
	}
	fr.addReport("nodeX", "reporterA")
	// 1 report, quorum = 3/2+1 = 2 → not enough
	if fr.hasQuorum("nodeX", 3, nodes) {
		t.Fatal("1 report should not reach quorum for 3 masters")
	}
	// 1 report, quorum = 1/2+1 = 1 → enough
	if !fr.hasQuorum("nodeX", 1, nodes) {
		t.Fatal("1 report should reach quorum for 1 master")
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
	// self + peer + master2 + master3 = 4 masters, quorum = 4/2+1 = 3
	master2 := newNode(genNodeID(), "127.0.0.1", 7002)
	master3 := newNode(genNodeID(), "127.0.0.1", 7003)
	s.mu.Lock()
	s.nodes[peer.id] = peer
	s.nodes[master2.id] = master2
	s.nodes[master3.id] = master3
	peer.flags |= flagPFail
	// Reporters must be masters in s.nodes for hasQuorum to count them.
	s.failureReports.addReport(peer.id, master2.id)
	s.failureReports.addReport(peer.id, master3.id)
	s.failureReports.addReport(peer.id, s.self.id)
	s.mu.Unlock()

	if !s.markNodeFail(peer.id) {
		t.Fatal("should mark node FAIL with quorum (3 master reports, need 3 for 4 masters)")
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
