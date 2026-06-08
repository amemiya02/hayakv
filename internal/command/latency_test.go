package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestLatencyLatestResetDoctor(t *testing.T) {
	c := connection.NewFakeConn()
	testServer.latencyMon.record("command", 5)
	ret := testServer.Exec(c, utils.ToCmdLine("LATENCY", "LATEST"))
	asserts.AssertNotError(t, ret)
	if !strings.Contains(string(ret.ToBytes()), "command") {
		t.Fatal("LATENCY LATEST missing event")
	}
	ret = testServer.Exec(c, utils.ToCmdLine("LATENCY", "RESET"))
	asserts.AssertNotError(t, ret)
	ret = testServer.Exec(c, utils.ToCmdLine("LATENCY", "DOCTOR"))
	asserts.AssertNotError(t, ret)
	if len(ret.ToBytes()) == 0 {
		t.Fatal("LATENCY DOCTOR empty")
	}
}

func TestLatencyHistory(t *testing.T) {
	c := connection.NewFakeConn()
	testServer.latencyMon.reset()
	testServer.latencyMon.record("command", 10)
	testServer.latencyMon.record("command", 20)
	ret := testServer.Exec(c, utils.ToCmdLine("LATENCY", "HISTORY", "command"))
	asserts.AssertNotError(t, ret)
	if !strings.Contains(string(ret.ToBytes()), "10") {
		t.Fatal("LATENCY HISTORY missing event data")
	}
}

func TestLatencyGraph(t *testing.T) {
	c := connection.NewFakeConn()
	testServer.latencyMon.reset()
	testServer.latencyMon.record("command", 3)
	ret := testServer.Exec(c, utils.ToCmdLine("LATENCY", "GRAPH", "command"))
	asserts.AssertNotError(t, ret)
	if !strings.Contains(string(ret.ToBytes()), "Latency command graph") {
		t.Fatal("LATENCY GRAPH missing header")
	}
}

func TestLatencyHelp(t *testing.T) {
	c := connection.NewFakeConn()
	ret := testServer.Exec(c, utils.ToCmdLine("LATENCY", "HELP"))
	asserts.AssertNotError(t, ret)
	if !strings.Contains(string(ret.ToBytes()), "LATENCY") {
		t.Fatal("LATENCY HELP missing content")
	}
}

func TestLatencyUnknownSubcommand(t *testing.T) {
	c := connection.NewFakeConn()
	ret := testServer.Exec(c, utils.ToCmdLine("LATENCY", "FOO"))
	asserts.AssertErrReply(t, ret, "ERR unknown subcommand 'FOO'")
}

func TestLatencyResetSpecific(t *testing.T) {
	c := connection.NewFakeConn()
	testServer.latencyMon.reset()
	testServer.latencyMon.record("command", 5)
	testServer.latencyMon.record("slow", 100)
	ret := testServer.Exec(c, utils.ToCmdLine("LATENCY", "RESET", "command"))
	asserts.AssertNotError(t, ret)
	// command should be gone, slow should remain
	evts := testServer.latencyMon.history("command")
	if len(evts) != 0 {
		t.Fatal("expected command events to be reset")
	}
	evts = testServer.latencyMon.history("slow")
	if len(evts) != 1 {
		t.Fatal("expected slow events to remain")
	}
	testServer.latencyMon.reset("slow")
}
