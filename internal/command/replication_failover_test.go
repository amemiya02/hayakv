package database

import (
	"strings"
	"testing"
)

func TestWaitAofReply(t *testing.T) {
	s := NewStandaloneServer()
	reply := execWaitAof(s, [][]byte{[]byte("0"), []byte("0"), []byte("100")})
	// Should return an array [local_fsynced, replicas_acked]
	// With appendonly off (default) and numlocal=0, local_fsynced should be 0.
	bytes := string(reply.ToBytes())
	if !strings.HasPrefix(bytes, "*") {
		t.Fatalf("WAITAOF should return an array, got: %q", bytes)
	}
	// Verify it returns [0, 0] — not [1, 0]
	if !strings.Contains(bytes, ":0\r\n") {
		t.Fatalf("WAITAOF 0 0 100 with appendonly off should return [0, ...], got: %q", bytes)
	}
}

func TestWaitAofNoAofError(t *testing.T) {
	// When appendonly is off and numlocal > 0, should error
	s := NewStandaloneServer()
	// Ensure AOF is off (default)
	reply := execWaitAof(s, [][]byte{[]byte("1"), []byte("0"), []byte("100")})
	bytes := string(reply.ToBytes())
	if !strings.Contains(bytes, "WAITAOF cannot be used") {
		t.Fatalf("expected error about appendonly disabled, got: %q", bytes)
	}
}

func TestWaitAofArgErrors(t *testing.T) {
	s := NewStandaloneServer()
	// Wrong number of args
	reply := execWaitAof(s, [][]byte{[]byte("0")})
	if !strings.Contains(string(reply.ToBytes()), "wrong number") {
		t.Fatalf("expected arg error, got: %q", string(reply.ToBytes()))
	}
}

func TestStandaloneFailoverRequiresMaster(t *testing.T) {
	s := NewStandaloneServer()
	// FAILOVER must be on a master (default role is masterRole)
	// But with no connected replicas, it should error.
	reply := execFailover(s, nil)
	if !strings.Contains(string(reply.ToBytes()), "No connected replicas") {
		t.Fatalf("expected 'No connected replicas' error, got: %q", string(reply.ToBytes()))
	}

	// A slave can't issue FAILOVER
	s.role = slaveRole
	reply = execFailover(s, nil)
	if !strings.Contains(string(reply.ToBytes()), "FAILOVER requires a master") {
		t.Fatalf("expected 'requires a master' error, got: %q", string(reply.ToBytes()))
	}
}

func TestStandaloneFailoverAbort(t *testing.T) {
	s := NewStandaloneServer()
	// ABORT is sent on the master (default role is masterRole)
	reply := execFailover(s, [][]byte{[]byte("ABORT")})
	if string(reply.ToBytes()) != "+OK\r\n" {
		t.Fatalf("expected OK for FAILOVER ABORT, got: %q", string(reply.ToBytes()))
	}
}

func TestReplid2SetOnPromotion(t *testing.T) {
	s := NewStandaloneServer()
	oldReplid := "oldmasterreplid0000000000000000000000000"
	s.promoteToMaster(oldReplid, 12345)

	info := string(genReplicationInfo(s))
	if strings.Contains(info, "master_replid2:0000000000000000000000000000000000000000") {
		t.Fatal("replid2 should be the old master's replid after promotion, not zeros")
	}
	if !strings.Contains(info, "master_replid2:"+oldReplid) {
		t.Fatalf("replid2 should be %q, got:\n%s", oldReplid, info)
	}
	if !strings.Contains(info, "second_repl_offset:12345") {
		t.Fatalf("second_repl_offset should be 12345, got:\n%s", info)
	}
}

func TestRoleFlipsToMasterOnPromotion(t *testing.T) {
	s := NewStandaloneServer()
	// Simulate being a slave first
	s.role = slaveRole
	s.promoteToMaster("someOldReplid", 100)
	if s.role != masterRole {
		t.Fatalf("expected masterRole (%d), got %d", masterRole, s.role)
	}
}

func TestPromoteToMasterGeneratesNewReplid(t *testing.T) {
	s := NewStandaloneServer()
	s.masterStatus.mu.RLock()
	origReplid := s.masterStatus.replId
	s.masterStatus.mu.RUnlock()

	s.promoteToMaster("oldreplid00000000000000000000000000000000", 500)

	s.masterStatus.mu.RLock()
	newReplid := s.masterStatus.replId
	s.masterStatus.mu.RUnlock()

	if newReplid == origReplid {
		t.Fatal("promoteToMaster should generate a new replId, but it stayed the same")
	}
	if len(newReplid) != 40 {
		t.Fatalf("new replId should be 40 chars, got %d: %q", len(newReplid), newReplid)
	}
}

// TestSlaveOfNoneSetsReplid2 guards the PSYNC2 regression: slaveOfNone must
// shift the old master's replid into replid2 (via promoteToMaster) so that
// sub-replicas can partial-resync after a failover.
func TestSlaveOfNoneSetsReplid2(t *testing.T) {
	s := NewStandaloneServer()
	// Simulate being a slave replicating from a master with a known replid.
	oldMasterReplid := "oldmasterreplid0000000000000000000000000"
	s.role = slaveRole
	s.slaveStatus.replId = oldMasterReplid
	s.slaveStatus.replOffset = 9999

	s.slaveOfNone()

	if s.role != masterRole {
		t.Fatalf("expected masterRole after slaveOfNone, got %d", s.role)
	}

	info := string(genReplicationInfo(s))
	if !strings.Contains(info, "master_replid2:"+oldMasterReplid) {
		t.Fatalf("replid2 should be %q after slaveOfNone, got:\n%s", oldMasterReplid, info)
	}
	if !strings.Contains(info, "second_repl_offset:9999") {
		t.Fatalf("second_repl_offset should be 9999 after slaveOfNone, got:\n%s", info)
	}
}
