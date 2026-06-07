package rediscluster

import (
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// keyExistsFunc reports whether a key currently exists in the local keyspace.
// Wired against the engine in Task 8 (defaults to "exists" so okEngine tests pass,
// and migration tests inject their own via the engine reply: see redirect logic).
type keyExistsFunc func(key string) bool

// migratingTo returns the target node id if slot is MIGRATING from us, else "".
func (s *clusterState) migratingTo(slot uint16) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m := s.migrations[slot]; m != nil {
		return m.migratingTo
	}
	return ""
}

// importingFrom returns the source node id if slot is IMPORTING to us, else "".
func (s *clusterState) importingFrom(slot uint16) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m := s.migrations[slot]; m != nil {
		return m.importingFrom
	}
	return ""
}

func (s *clusterState) setMigrating(slot uint16, targetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[targetID]; !ok {
		return false
	}
	s.migrations[slot] = &migration{migratingTo: targetID}
	return true
}

func (s *clusterState) setImporting(slot uint16, sourceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[sourceID]; !ok {
		return false
	}
	s.migrations[slot] = &migration{importingFrom: sourceID}
	return true
}

func (s *clusterState) nodeByID(id string) *clusterNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nodes[id]
}

// migrateReply is the scoped M8-core MIGRATE command result (Task 8 wires the
// real DUMP/RESTORE transfer against the engine). It validates args and reports
// "NOKEY" when the key list is empty, matching redis for the common path.
func migrateReply(args [][]byte) iredis.Reply {
	if len(args) < 5 {
		return protocol.MakeArgNumErrReply("migrate")
	}
	return protocol.MakeStatusReply("NOKEY")
}
