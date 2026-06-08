package rediscluster

import (
	"sync"
	"time"
)

// failoverState tracks the state of an ongoing failover election on a replica.
type failoverState struct {
	mu             sync.Mutex
	active         bool
	authTimeout    time.Time       // deadline for collecting votes
	votesReceived  map[string]bool // nodeID of masters who voted for us
	votesNeeded    int             // majority of masters
	reqEpoch       uint64          // the epoch we requested
	failedMasterID string          // the master we're replacing
}

// grantVote decides whether this master should grant a vote to a requesting replica.
// Rules (Redis cluster spec):
//  1. reqEpoch must be >= our currentEpoch
//  2. We must not have already voted in this epoch (lastVoteEpoch < reqEpoch)
//  3. The requesting replica's master must be marked FAIL
//  4. The sender must be a replica of the failed master
//
// Returns true if vote was granted.
func (s *clusterState) grantVote(replicaID, failedMasterID string, reqEpoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rule 1: reqEpoch must be >= currentEpoch
	if reqEpoch < s.epoch {
		return false
	}

	// Rule 2: not yet voted this epoch
	if s.lastVoteEpoch >= reqEpoch {
		return false
	}

	// Rule 3: the master must be FAIL
	master := s.nodes[failedMasterID]
	if master == nil || master.flags&flagFail == 0 {
		return false
	}

	// Rule 4: the requester must be a replica of the failed master
	replica := s.nodes[replicaID]
	if replica == nil || replica.masterID != failedMasterID {
		return false
	}

	// Grant the vote
	s.lastVoteEpoch = reqEpoch
	return true
}

// masterCount returns the number of master nodes in the cluster.
func (s *clusterState) masterCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, n := range s.nodes {
		if n.flags&flagMaster != 0 {
			count++
		}
	}
	return count
}

// startElection begins a failover election on this replica node.
// It bumps the current epoch, broadcasts AUTH_REQUEST, and waits for votes.
func (s *clusterState) startElection(failedMasterID string, bus *gossipBus) {
	s.mu.Lock()
	// Bump epoch
	s.epoch++
	reqEpoch := s.epoch
	s.mu.Unlock()

	// Determine quorum (majority of masters)
	masters := s.masterCount()
	votesNeeded := masters/2 + 1

	// Initialize failover state. Do NOT self-seed: the candidate is a replica,
	// not a master, so its own vote doesn't count toward the master quorum.
	// Redis requires votesNeeded master votes (majority of masters).
	fs := &failoverState{
		active:         true,
		authTimeout:    time.Now().Add(2 * time.Second),
		votesReceived:  map[string]bool{},
		votesNeeded:    votesNeeded,
		reqEpoch:       reqEpoch,
		failedMasterID: failedMasterID,
	}

	s.mu.Lock()
	s.failoverState = fs
	s.mu.Unlock()

	// Broadcast AUTH_REQUEST
	if bus != nil {
		bus.broadcastMessage(msgTypeFailoverAuthRequest)
	}
}

// recordVote records a vote (AUTH_ACK) from a master.
// Returns true if the election is won (quorum reached).
// voteEpoch must match the current election's reqEpoch; stale votes are ignored.
func (s *clusterState) recordVote(voterID string, voteEpoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	fs := s.failoverState
	if fs == nil || !fs.active {
		return false
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Ignore votes for a different election epoch (stale or future).
	if voteEpoch != fs.reqEpoch {
		return false
	}

	if fs.votesReceived[voterID] {
		return false // duplicate vote
	}
	fs.votesReceived[voterID] = true

	return len(fs.votesReceived) >= fs.votesNeeded
}

// claimOwnership promotes this replica to master and claims the failed master's slots.
// Called when the election is won. Does NOT bump epoch — startElection already did.
func (s *clusterState) claimOwnership(failedMasterID string) {
	s.mu.Lock()
	failedMaster, ok := s.nodes[failedMasterID]
	if !ok {
		s.mu.Unlock()
		return
	}

	// Flip self from replica to master
	s.self.flags &^= flagSlave
	s.self.flags |= flagMaster
	s.self.masterID = ""
	s.self.configEpoch = s.epoch

	// Take over the failed master's slots
	for slot := uint16(0); slot < slotCount; slot++ {
		if failedMaster.hasSlot(slot) {
			failedMaster.delSlot(slot)
			s.slots[slot] = s.self
			s.self.addSlot(slot)
		}
	}

	// Clear failover state
	if s.failoverState != nil {
		s.failoverState.mu.Lock()
		s.failoverState.active = false
		s.failoverState.mu.Unlock()
	}
	s.mu.Unlock()

	// Persist (separate lock acquisition — save() takes RLock)
	_ = s.save()
}

// clusterHandleConfigEpochCollision resolves config-epoch collisions:
// if two nodes have the same configEpoch, the one with the smaller node ID
// must bump its epoch.
func (s *clusterState) clusterHandleConfigEpochCollision() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n1 := range s.nodes {
		if n1.id == s.self.id {
			continue
		}
		if n1.configEpoch == s.self.configEpoch && n1.id != s.self.id {
			if s.self.id < n1.id {
				// We have the smaller ID — we must bump
				s.epoch++
				s.self.configEpoch = s.epoch
			}
		}
	}
}

// checkFailoverTick is called from the gossip ping loop to check whether
// this replica should trigger a failover election.
func (s *clusterState) checkFailoverTick(bus *gossipBus) {
	s.mu.RLock()
	isReplica := s.self.flags&flagSlave != 0
	masterID := s.self.masterID
	fs := s.failoverState
	s.mu.RUnlock()

	if !isReplica || masterID == "" {
		return
	}

	// Check if our master is FAIL
	s.mu.RLock()
	master := s.nodes[masterID]
	masterIsFail := master != nil && master.flags&flagFail != 0
	s.mu.RUnlock()

	if !masterIsFail {
		return
	}

	// If we're already running an election, check if we won.
	// Read fs.active under fs.mu to avoid racing with claimOwnership.
	if fs != nil {
		fs.mu.Lock()
		active := fs.active
		won := active && len(fs.votesReceived) >= fs.votesNeeded
		timedOut := active && time.Now().After(fs.authTimeout)
		fs.mu.Unlock()

		if active {
			if won {
				s.claimOwnership(masterID)
				// Broadcast our new state
				if bus != nil {
					bus.broadcastMessage(msgTypePong)
				}
			} else if timedOut {
				// Election failed, reset
				s.mu.Lock()
				if s.failoverState != nil {
					s.failoverState.mu.Lock()
					s.failoverState.active = false
					s.failoverState.mu.Unlock()
				}
				s.mu.Unlock()
			}
			return
		}
		// Election already concluded (active=false); fall through to
		// start a new election below.
	}

	// Start a new election with a delay based on replication rank.
	// The delay staggers replicas so they don't all start simultaneously.
	// Use a proper hash of the nodeID for wide jitter spread.
	s.mu.RLock()
	jitter := nodeIDJitter(s.self.id)
	s.mu.RUnlock()
	time.Sleep(time.Duration(500+jitter) * time.Millisecond)

	// Re-check: another replica may have already won.
	s.mu.RLock()
	master = s.nodes[masterID]
	stillFail := master != nil && master.flags&flagFail != 0
	s.mu.RUnlock()
	if !stillFail {
		return
	}

	s.startElection(masterID, bus)
}

// nodeIDJitter returns a deterministic jitter in [0, 500)ms based on the
// node ID. Uses a simple hash of all hex chars for wide spread.
func nodeIDJitter(id string) int64 {
	var sum uint32
	for i := 0; i < len(id); i++ {
		sum = sum*31 + uint32(id[i])
	}
	return int64(sum % 500)
}
