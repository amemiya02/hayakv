package database

import (
	"strings"
	"testing"
)

func TestGenReplicationInfoMaster(t *testing.T) {
	server := &Server{}
	server.slaveStatus = initReplSlaveStatus()
	server.initMasterStatus()
	server.role = masterRole

	s := string(genReplicationInfo(server))
	for _, want := range []string{
		"# Replication\r\n",
		"role:master\r\n",
		"connected_slaves:0\r\n",
		"master_failover_state:no-failover\r\n",
		"master_replid:",
		"master_repl_offset:",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("master INFO replication missing %q in:\n%s", want, s)
		}
	}
	// master_replid must be 40 hex chars.
	line := extractField(s, "master_replid:")
	if len(line) != 40 {
		t.Fatalf("master_replid len = %d (%q), want 40", len(line), line)
	}
}

func TestGenReplicationInfoSlave(t *testing.T) {
	server := &Server{}
	server.slaveStatus = initReplSlaveStatus()
	server.initMasterStatus()
	server.role = slaveRole
	server.slaveStatus.masterHost = "127.0.0.1"
	server.slaveStatus.masterPort = 6380
	server.slaveStatus.replOffset = 123

	s := string(genReplicationInfo(server))
	for _, want := range []string{
		"role:slave\r\n",
		"master_host:127.0.0.1\r\n",
		"master_port:6380\r\n",
		"master_link_status:",
		"slave_read_only:1\r\n",
		"slave_repl_offset:123\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("slave INFO replication missing %q in:\n%s", want, s)
		}
	}
}

// extractField returns the value (without CRLF) following the given prefix.
func extractField(s, prefix string) string {
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	if j := strings.Index(rest, "\r\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}
