package rediscluster

import (
	"testing"
)

func TestVoteGrantedOncePerEpoch(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")
	s.epoch = 5

	failedMasterID := genNodeID()
	replicaID := genNodeID()

	failedMaster := newNode(failedMasterID, "127.0.0.1", 7001)
	replica := &clusterNode{id: replicaID, ip: "127.0.0.1", port: 7002, flags: flagSlave, masterID: failedMasterID, linkUp: true}

	s.mu.Lock()
	s.nodes[failedMasterID] = failedMaster
	s.nodes[replicaID] = replica
	failedMaster.flags |= flagFail
	s.mu.Unlock()

	ok1 := s.grantVote(replicaID, failedMasterID, 6)
	ok2 := s.grantVote(replicaID, failedMasterID, 6)

	if !ok1 {
		t.Fatal("first vote should be granted")
	}
	if ok2 {
		t.Fatal("second vote in same epoch should be denied")
	}
	if s.lastVoteEpoch != 6 {
		t.Fatalf("lastVoteEpoch = %d, want 6", s.lastVoteEpoch)
	}
}

func TestVoteDeniedIfMasterNotFail(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")
	s.epoch = 5

	masterID := genNodeID()
	replicaID := genNodeID()

	master := newNode(masterID, "127.0.0.1", 7001)
	replica := &clusterNode{id: replicaID, ip: "127.0.0.1", port: 7002, flags: flagSlave, masterID: masterID, linkUp: true}

	s.mu.Lock()
	s.nodes[masterID] = master
	s.nodes[replicaID] = replica
	s.mu.Unlock()

	ok := s.grantVote(replicaID, masterID, 6)
	if ok {
		t.Fatal("vote should be denied when master is not FAIL")
	}
}

func TestVoteDeniedForOldEpoch(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")
	s.epoch = 10

	ok := s.grantVote("someReplica", "someMaster", 5)
	if ok {
		t.Fatal("vote should be denied for old epoch")
	}
}

func TestClaimOwnership(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")

	masterID := genNodeID()
	master := newNode(masterID, "127.0.0.1", 7001)
	master.addSlot(0)
	master.addSlot(1)
	master.addSlot(2)

	s.mu.Lock()
	s.nodes[masterID] = master
	s.self.flags &^= flagMaster
	s.self.flags |= flagSlave
	s.self.masterID = masterID
	s.slots[0] = master
	s.slots[1] = master
	s.slots[2] = master
	s.mu.Unlock()

	s.claimOwnership(masterID)

	s.mu.RLock()
	isMaster := s.self.flags&flagMaster != 0
	isSlave := s.self.flags&flagSlave != 0
	ownsSlot0 := s.slots[0] == s.self
	ownsSlot1 := s.slots[1] == s.self
	ownsSlot2 := s.slots[2] == s.self
	noMasterID := s.self.masterID == ""
	s.mu.RUnlock()

	if !isMaster {
		t.Fatal("self should be master after claimOwnership")
	}
	if isSlave {
		t.Fatal("self should not be slave after claimOwnership")
	}
	if !ownsSlot0 || !ownsSlot1 || !ownsSlot2 {
		t.Fatal("self should own the failed master's slots")
	}
	if !noMasterID {
		t.Fatal("self should have no masterID after claimOwnership")
	}
}

func TestConfigEpochCollision(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")

	peerID := genNodeID()
	peer := newNode(peerID, "127.0.0.1", 7001)

	s.mu.Lock()
	s.self.configEpoch = 5
	peer.configEpoch = 5
	s.nodes[peerID] = peer
	s.mu.Unlock()

	s.clusterHandleConfigEpochCollision()

	s.mu.RLock()
	selfEpoch := s.self.configEpoch
	s.mu.RUnlock()

	if s.myID() < peerID && selfEpoch <= 5 {
		t.Fatalf("expected bumped epoch > 5 for smaller node ID, got %d", selfEpoch)
	}
	if s.myID() > peerID && selfEpoch != 5 {
		t.Fatalf("expected unchanged epoch 5 for larger node ID, got %d", selfEpoch)
	}
}

func TestRecordVote(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")

	s.mu.Lock()
	s.self.flags &^= flagMaster
	s.self.flags |= flagSlave
	s.failoverState = &failoverState{
		active:        true,
		votesReceived: map[string]bool{s.self.id: true},
		votesNeeded:   2,
		reqEpoch:      10,
	}
	s.mu.Unlock()

	// Vote with matching epoch → should count
	won := s.recordVote("voter1", 10)
	if !won {
		t.Fatal("election should be won with 2 votes when 2 needed")
	}

	// Duplicate vote → ignored
	won2 := s.recordVote("voter1", 10)
	if won2 {
		t.Fatal("duplicate vote should not re-trigger win")
	}
}

func TestRecordVoteRejectsStaleEpoch(t *testing.T) {
	s := newClusterState("127.0.0.1", 7000, t.TempDir()+"/nodes.conf")

	s.mu.Lock()
	s.self.flags &^= flagMaster
	s.self.flags |= flagSlave
	s.failoverState = &failoverState{
		active:        true,
		votesReceived: map[string]bool{s.self.id: true},
		votesNeeded:   2,
		reqEpoch:      10,
	}
	s.mu.Unlock()

	// Vote with stale epoch → should be rejected
	won := s.recordVote("voter1", 5)
	if won {
		t.Fatal("stale epoch vote should be rejected")
	}

	// Vote with future epoch → should also be rejected
	won = s.recordVote("voter1", 15)
	if won {
		t.Fatal("future epoch vote should be rejected")
	}
}
