package rediscluster

import (
	"time"
)

// failureReports tracks PFAIL/FAIL reports for cluster nodes.
// Per the Redis cluster spec, when a majority of masters agree a node has failed
// (PFAIL → FAIL), the node is marked as definitively failed.
type failureReports struct {
	// m maps nodeID → reporterID → timestamp (unix ms)
	m      map[string]map[string]int64
	quorum int
}

func newFailureReports(quorum int) *failureReports {
	return &failureReports{
		m:      make(map[string]map[string]int64),
		quorum: quorum,
	}
}

// addReport records that reporterID claims nodeID is PFAIL at the given time.
func (fr *failureReports) addReport(nodeID, reporterID string) {
	now := time.Now().UnixMilli()
	if fr.m[nodeID] == nil {
		fr.m[nodeID] = make(map[string]int64)
	}
	fr.m[nodeID][reporterID] = now
}

// count returns the number of distinct reporters claiming nodeID is PFAIL.
func (fr *failureReports) count(nodeID string) int {
	return len(fr.m[nodeID])
}

// hasQuorum reports whether nodeID has enough PFAIL reports to be marked FAIL.
func (fr *failureReports) hasQuorum(nodeID string) bool {
	return fr.count(nodeID) >= fr.quorum
}

// cleanup removes reports older than 2*nodeTimeout for a given node.
func (fr *failureReports) cleanup(nodeID string, nodeTimeoutMs int64) {
	reports, ok := fr.m[nodeID]
	if !ok {
		return
	}
	cutoff := time.Now().UnixMilli() - 2*nodeTimeoutMs
	for reporter, ts := range reports {
		if ts < cutoff {
			delete(reports, reporter)
		}
	}
	if len(reports) == 0 {
		delete(fr.m, nodeID)
	}
}

// markPFailIfTimedOut checks all known peers and marks any as PFAIL
// whose last pong is older than nodeTimeoutMs. Called from the gossip ping loop.
func (s *clusterState) markPFailIfTimedOut(nodeTimeoutMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	for _, n := range s.nodes {
		if n.id == s.self.id {
			continue
		}
		if n.flags&flagFail != 0 {
			continue // already FAIL
		}
		if n.pongRecv > 0 && now-n.pongRecv > nodeTimeoutMs {
			n.flags |= flagPFail
			// Record our own PFAIL report for this node
			if s.failureReports != nil {
				s.failureReports.addReport(n.id, s.self.id)
			}
		}
	}
}

// markNodeFail promotes a node from PFAIL to FAIL if quorum of masters agree.
// Returns true if the node was newly marked FAIL.
func (s *clusterState) markNodeFail(nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[nodeID]
	if !ok || n.flags&flagFail != 0 {
		return false
	}
	// Count how many masters (excluding self) have PFAIL reports for this node
	if s.failureReports != nil && s.failureReports.hasQuorum(nodeID) {
		n.flags &^= flagPFail
		n.flags |= flagFail
		return true
	}
	return false
}
