package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestMonitorRepliesOK(t *testing.T) {
	s := NewStandaloneServer()
	mon := connection.NewFakeConn()

	result := s.Exec(mon, utils.ToCmdLine("MONITOR"))
	asserts.AssertStatusReply(t, result, "OK")
}

func TestMonitorFeeds(t *testing.T) {
	// Clear any leftover monitors from other tests
	monitorConnsMu.Lock()
	monitorConns = make(map[uint64]redis.Connection)
	monitorConnsMu.Unlock()

	s := NewStandaloneServer()
	mon := connection.NewFakeConn()

	// Start monitoring
	s.Exec(mon, utils.ToCmdLine("MONITOR"))

	// Execute a command on another connection
	other := connection.NewFakeConn()
	s.Exec(other, utils.ToCmdLine("SET", "mykey", "myval"))

	// The monitor connection should have received a feed
	written := mon.Bytes()
	if len(written) == 0 {
		t.Fatal("monitor did not receive any feed")
	}
	str := string(written)
	if !strings.Contains(str, "SET") {
		t.Fatalf("monitor feed should contain SET, got: %q", str)
	}
	if !strings.Contains(str, "mykey") {
		t.Fatalf("monitor feed should contain mykey, got: %q", str)
	}
}

func TestResetReplies(t *testing.T) {
	s := NewStandaloneServer()
	c := connection.NewFakeConn()

	result := s.Exec(c, utils.ToCmdLine("RESET"))
	asserts.AssertStatusReply(t, result, "RESET")
}

func TestResetClearsDB(t *testing.T) {
	s := NewStandaloneServer()
	c := connection.NewFakeConn()

	// Select DB 3
	s.Exec(c, utils.ToCmdLine("SELECT", "3"))
	if c.GetDBIndex() != 3 {
		t.Fatalf("expected DB 3, got %d", c.GetDBIndex())
	}

	// Reset
	s.Exec(c, utils.ToCmdLine("RESET"))
	if c.GetDBIndex() != 0 {
		t.Fatalf("after RESET expected DB 0, got %d", c.GetDBIndex())
	}
}

func TestResetClearsMultiState(t *testing.T) {
	s := NewStandaloneServer()
	c := connection.NewFakeConn()

	// Enter MULTI
	s.Exec(c, utils.ToCmdLine("MULTI"))
	if !c.InMultiState() {
		t.Fatal("expected MULTI state")
	}

	// Reset
	s.Exec(c, utils.ToCmdLine("RESET"))
	if c.InMultiState() {
		t.Fatal("after RESET expected no MULTI state")
	}
}

func TestResetClearsClientName(t *testing.T) {
	s := NewStandaloneServer()
	c := connection.NewFakeConn()

	c.SetClientName("myname")
	if c.ClientName() != "myname" {
		t.Fatalf("expected client name 'myname', got %q", c.ClientName())
	}

	// Reset
	s.Exec(c, utils.ToCmdLine("RESET"))
	if c.ClientName() != "" {
		t.Fatalf("after RESET expected empty client name, got %q", c.ClientName())
	}
}

func TestMonitorRemovedOnClose(t *testing.T) {
	// Clear any leftover monitors
	monitorConnsMu.Lock()
	monitorConns = make(map[uint64]redis.Connection)
	monitorConnsMu.Unlock()

	s := NewStandaloneServer()
	mon := connection.NewFakeConn()

	// Register monitor
	s.Exec(mon, utils.ToCmdLine("MONITOR"))

	monitorConnsMu.RLock()
	count := len(monitorConns)
	monitorConnsMu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 monitor, got %d", count)
	}

	// AfterClientClose should unregister
	s.AfterClientClose(mon)

	monitorConnsMu.RLock()
	count = len(monitorConns)
	monitorConnsMu.RUnlock()
	if count != 0 {
		t.Fatalf("expected 0 monitors after close, got %d", count)
	}
}
