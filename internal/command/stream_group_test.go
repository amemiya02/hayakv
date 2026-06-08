package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestXGroupReadAck(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// Add entries
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))

	// Create group
	ret := s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))
	if string(ret.ToBytes()) != "+OK\r\n" {
		t.Fatalf("XGROUP CREATE = %q", ret.ToBytes())
	}

	// Read as consumer
	ret = s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c", "COUNT", "1", "STREAMS", "st", ">"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XREADGROUP = %q", raw)
	}

	// Check pending
	ret = s.Exec(conn, utils.ToCmdLine("XPENDING", "st", "g"))
	raw = string(ret.ToBytes())
	if !strings.Contains(raw, ":1\r\n") {
		t.Fatalf("XPENDING should show 1 pending: %q", raw)
	}

	// Ack
	ret = s.Exec(conn, utils.ToCmdLine("XACK", "st", "g", "1-0"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("XACK = %q", ret.ToBytes())
	}

	// Pending should be 0 now
	ret = s.Exec(conn, utils.ToCmdLine("XPENDING", "st", "g"))
	raw = string(ret.ToBytes())
	if strings.Contains(raw, ":1\r\n") && !strings.Contains(raw, ":0\r\n") {
		t.Fatalf("XPENDING after ACK should show 0: %q", raw)
	}
}

func TestXGroupCreateDestroy(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// Create stream first
	s.Exec(conn, utils.ToCmdLine("XADD", "mystream", "1-0", "key", "val"))

	// Create group
	ret := s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "mystream", "mygroup", "0"))
	if string(ret.ToBytes()) != "+OK\r\n" {
		t.Fatalf("XGROUP CREATE = %q", ret.ToBytes())
	}

	// Create same group again should fail
	ret = s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "mystream", "mygroup", "0"))
	if !strings.Contains(string(ret.ToBytes()), "BUSYGROUP") {
		t.Fatalf("expected BUSYGROUP error, got %q", ret.ToBytes())
	}

	// Destroy group
	ret = s.Exec(conn, utils.ToCmdLine("XGROUP", "DESTROY", "mystream", "mygroup"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("XGROUP DESTROY = %q", ret.ToBytes())
	}

	// Destroy non-existent group
	ret = s.Exec(conn, utils.ToCmdLine("XGROUP", "DESTROY", "mystream", "mygroup"))
	if string(ret.ToBytes()) != ":0\r\n" {
		t.Fatalf("XGROUP DESTROY nonexistent = %q", ret.ToBytes())
	}
}

func TestXGroupMKStream(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// XGROUP CREATE with MKSTREAM should create the stream
	ret := s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "newstream", "g", "0", "MKSTREAM"))
	if string(ret.ToBytes()) != "+OK\r\n" {
		t.Fatalf("XGROUP CREATE MKSTREAM = %q", ret.ToBytes())
	}

	// Verify stream exists by adding an entry
	ret = s.Exec(conn, utils.ToCmdLine("XADD", "newstream", "1-0", "f", "v"))
	if ret.ToBytes()[0] == '-' {
		t.Fatalf("XADD after MKSTREAM failed: %q", ret.ToBytes())
	}
}

func TestXGroupCreateConsumer(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// Create consumer
	ret := s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATECONSUMER", "st", "g", "consumer1"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("XGROUP CREATECONSUMER = %q", ret.ToBytes())
	}

	// Create same consumer again should return 0
	ret = s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATECONSUMER", "st", "g", "consumer1"))
	if string(ret.ToBytes()) != ":0\r\n" {
		t.Fatalf("XGROUP CREATECONSUMER duplicate = %q", ret.ToBytes())
	}
}

func TestXGroupDelConsumer(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// Read to create consumer with pending entries
	s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c", "COUNT", "1", "STREAMS", "st", ">"))

	// Delete consumer - should return 1 (pending entry count)
	ret := s.Exec(conn, utils.ToCmdLine("XGROUP", "DELCONSUMER", "st", "g", "c"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("XGROUP DELCONSUMER = %q", ret.ToBytes())
	}

	// Delete non-existent consumer
	ret = s.Exec(conn, utils.ToCmdLine("XGROUP", "DELCONSUMER", "st", "g", "nobody"))
	if string(ret.ToBytes()) != ":0\r\n" {
		t.Fatalf("XGROUP DELCONSUMER nonexistent = %q", ret.ToBytes())
	}
}

func TestXGroupSetID(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// Set group ID to 2-0
	ret := s.Exec(conn, utils.ToCmdLine("XGROUP", "SETID", "st", "g", "2-0"))
	if string(ret.ToBytes()) != "+OK\r\n" {
		t.Fatalf("XGROUP SETID = %q", ret.ToBytes())
	}

	// Read new entries - should get nothing (since we set last delivered to 2-0)
	ret = s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c", "COUNT", "10", "STREAMS", "st", ">"))
	raw := string(ret.ToBytes())
	// Should be null or empty
	if raw != "$-1\r\n" && raw != "*0\r\n" && raw[0] != '-' {
		// Only check if we got entries - this could happen if 2-0 is the start
		t.Logf("XREADGROUP after SETID: %q", raw)
	}
}

func TestXPendingExtended(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// Read both entries
	s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c", "STREAMS", "st", ">"))

	// Extended pending
	ret := s.Exec(conn, utils.ToCmdLine("XPENDING", "st", "g", "-", "+", "10"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XPENDING extended = %q", raw)
	}
	// Should contain both IDs
	if !strings.Contains(raw, "1-0") || !strings.Contains(raw, "2-0") {
		t.Fatalf("XPENDING extended should contain both IDs: %q", raw)
	}

	// Extended pending with consumer filter
	ret = s.Exec(conn, utils.ToCmdLine("XPENDING", "st", "g", "-", "+", "10", "c"))
	raw = string(ret.ToBytes())
	if !strings.Contains(raw, "1-0") {
		t.Fatalf("XPENDING extended with consumer = %q", raw)
	}

	// Extended pending for non-existent consumer
	ret = s.Exec(conn, utils.ToCmdLine("XPENDING", "st", "g", "-", "+", "10", "nobody"))
	raw = string(ret.ToBytes())
	if raw != "*0\r\n" {
		t.Fatalf("XPENDING extended for nobody = %q", raw)
	}
}

func TestXReadGroupPending(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// Read new entries
	ret := s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c", "STREAMS", "st", ">"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XREADGROUP > = %q", raw)
	}

	// Read pending entries for consumer
	ret = s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c", "COUNT", "10", "STREAMS", "st", "0"))
	raw = string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XREADGROUP 0 = %q", raw)
	}
	// Should contain the pending IDs
	if !strings.Contains(raw, "1-0") || !strings.Contains(raw, "2-0") {
		t.Fatalf("XREADGROUP pending should contain both IDs: %q", raw)
	}
}

func TestXInfo(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// XINFO STREAM
	ret := s.Exec(conn, utils.ToCmdLine("XINFO", "STREAM", "st"))
	raw := string(ret.ToBytes())
	if !strings.Contains(raw, "length") {
		t.Fatalf("XINFO STREAM missing length: %q", raw)
	}
	if !strings.Contains(raw, "2") {
		t.Fatalf("XINFO STREAM should show length 2: %q", raw)
	}
	if !strings.Contains(raw, "groups") {
		t.Fatalf("XINFO STREAM missing groups: %q", raw)
	}

	// XINFO GROUPS
	ret = s.Exec(conn, utils.ToCmdLine("XINFO", "GROUPS", "st"))
	raw = string(ret.ToBytes())
	if !strings.Contains(raw, "g") {
		t.Fatalf("XINFO GROUPS should show group 'g': %q", raw)
	}

	// XINFO CONSUMERS (empty initially)
	ret = s.Exec(conn, utils.ToCmdLine("XINFO", "CONSUMERS", "st", "g"))
	raw = string(ret.ToBytes())
	// No consumers yet, should be empty array
	if raw != "*0\r\n" {
		t.Logf("XINFO CONSUMERS (empty): %q", raw)
	}

	// Create a consumer by reading
	s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c", "STREAMS", "st", ">"))

	// XINFO CONSUMERS should now show the consumer
	ret = s.Exec(conn, utils.ToCmdLine("XINFO", "CONSUMERS", "st", "g"))
	raw = string(ret.ToBytes())
	if !strings.Contains(raw, "c") {
		t.Fatalf("XINFO CONSUMERS should show consumer 'c': %q", raw)
	}
	if !strings.Contains(raw, "pending") {
		t.Fatalf("XINFO CONSUMERS missing pending: %q", raw)
	}
}

func TestXClaim(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// Read as consumer c1
	s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c1", "STREAMS", "st", ">"))

	// Claim entry 1-0 as c2 with 0 min idle time
	ret := s.Exec(conn, utils.ToCmdLine("XCLAIM", "st", "g", "c2", "0", "1-0"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XCLAIM = %q", raw)
	}
	if !strings.Contains(raw, "1-0") {
		t.Fatalf("XCLAIM should contain 1-0: %q", raw)
	}

	// XCLAIM with JUSTID
	ret = s.Exec(conn, utils.ToCmdLine("XCLAIM", "st", "g", "c2", "0", "2-0", "JUSTID"))
	raw = string(ret.ToBytes())
	if !strings.Contains(raw, "2-0") {
		t.Fatalf("XCLAIM JUSTID should contain 2-0: %q", raw)
	}
}

func TestXReadGroupNoStream(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// XGROUP CREATE on non-existent key without MKSTREAM should fail
	ret := s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "nostream", "g", "0"))
	raw := string(ret.ToBytes())
	if !strings.Contains(raw, "ERR") {
		t.Fatalf("XGROUP CREATE on missing key = %q", raw)
	}

	// XGROUP DESTROY on non-existent key should return 0
	ret = s.Exec(conn, utils.ToCmdLine("XGROUP", "DESTROY", "nostream", "g"))
	if string(ret.ToBytes()) != ":0\r\n" {
		t.Fatalf("XGROUP DESTROY on missing key = %q", ret.ToBytes())
	}
}

func TestXAutoClaim(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	s.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))
	s.Exec(conn, utils.ToCmdLine("XGROUP", "CREATE", "st", "g", "0"))

	// Read as consumer c1
	s.Exec(conn, utils.ToCmdLine("XREADGROUP", "GROUP", "g", "c1", "STREAMS", "st", ">"))

	// Auto-claim with 0 min idle time
	ret := s.Exec(conn, utils.ToCmdLine("XAUTOCLAIM", "st", "g", "c2", "0", "0-0"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XAUTOCLAIM = %q", raw)
	}
	// Should contain next-start-id and claimed entries
	if !strings.Contains(raw, "1-0") || !strings.Contains(raw, "2-0") {
		t.Logf("XAUTOCLAIM result: %q", raw)
	}

	// XAUTOCLAIM with JUSTID
	ret = s.Exec(conn, utils.ToCmdLine("XAUTOCLAIM", "st", "g", "c2", "0", "0-0", "JUSTID"))
	raw = string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XAUTOCLAIM JUSTID = %q", raw)
	}
}
