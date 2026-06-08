package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestDebugReloadPreservesDigest(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "a", "1"))
	s.Exec(conn, utils.ToCmdLine("RPUSH", "l", "x", "y"))
	before := datasetDigest(s)
	if r := s.Exec(conn, utils.ToCmdLine("DEBUG", "RELOAD")); string(r.ToBytes()) != "+OK\r\n" {
		t.Fatalf("DEBUG RELOAD = %q", r.ToBytes())
	}
	if after := datasetDigest(s); before != after {
		t.Fatalf("digest changed across RELOAD: %x vs %x", before, after)
	}
}

func TestDebugDigest(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "k", "v"))
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "DIGEST"))
	if protocol.IsErrorReply(r) {
		t.Fatalf("DEBUG DIGEST = %q", r.ToBytes())
	}
	// Should be hex-encoded 40 chars
	status := strings.TrimPrefix(string(r.ToBytes()), "+")
	status = strings.TrimSuffix(status, "\r\n")
	if len(status) != 40 {
		t.Fatalf("digest hex length = %d, want 40: %q", len(status), status)
	}
}

func TestDebugDigestValue(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "k1", "v1"))
	s.Exec(conn, utils.ToCmdLine("SET", "k2", "v2"))
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "DIGEST-VALUE", "k1", "k2", "missing"))
	if protocol.IsErrorReply(r) {
		t.Fatalf("DEBUG DIGEST-VALUE = %q", r.ToBytes())
	}
	// Should return 3 elements: 2 status + 1 null
	bytes := r.ToBytes()
	if !strings.HasPrefix(string(bytes), "*3\r\n") {
		t.Fatalf("expected 3-element array, got: %q", bytes)
	}
}

func TestDebugError(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	if r := s.Exec(conn, utils.ToCmdLine("DEBUG", "SET-ACTIVE-EXPIRE", "0")); string(r.ToBytes()) != "+OK\r\n" {
		t.Fatalf("SET-ACTIVE-EXPIRE = %q", r.ToBytes())
	}
	if r := s.Exec(conn, utils.ToCmdLine("DEBUG", "ERROR", "boom")); string(r.ToBytes()) != "-boom\r\n" {
		t.Fatalf("DEBUG ERROR = %q", r.ToBytes())
	}
}

func TestDebugSleep(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "SLEEP", "0"))
	if string(r.ToBytes()) != "+OK\r\n" {
		t.Fatalf("DEBUG SLEEP = %q", r.ToBytes())
	}
}

func TestDebugPopulate(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "POPULATE", "10", "test:", "5"))
	if string(r.ToBytes()) != "+OK\r\n" {
		t.Fatalf("DEBUG POPULATE = %q", r.ToBytes())
	}
	// Verify keys were created
	r = s.Exec(conn, utils.ToCmdLine("DBSIZE"))
	if protocol.IsErrorReply(r) {
		t.Fatalf("DBSIZE = %q", r.ToBytes())
	}
}

func TestDebugProtocol(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	types := []string{"STRING", "INTEGER", "DOUBLE", "BIGNUM", "NULL", "ARRAY", "SET", "MAP", "TRUE", "FALSE", "ERR", "PUSH"}
	for _, typeName := range types {
		r := s.Exec(conn, utils.ToCmdLine("DEBUG", "PROTOCOL", typeName))
		if len(r.ToBytes()) == 0 {
			t.Fatalf("DEBUG PROTOCOL %s returned empty", typeName)
		}
	}
}

func TestDebugObject(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	s.Exec(conn, utils.ToCmdLine("SET", "mykey", "myvalue"))
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "OBJECT", "mykey"))
	if protocol.IsErrorReply(r) {
		t.Fatalf("DEBUG OBJECT = %q", r.ToBytes())
	}
	body := string(r.ToBytes())
	if !strings.Contains(body, "encoding:") {
		t.Fatalf("DEBUG OBJECT missing encoding: %q", body)
	}
	if !strings.Contains(body, "serializedlength:") {
		t.Fatalf("DEBUG OBJECT missing serializedlength: %q", body)
	}
}

func TestDebugQuicklistPackedThreshold(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "QUICKLIST-PACKED-THRESHOLD", "128"))
	if string(r.ToBytes()) != "+OK\r\n" {
		t.Fatalf("DEBUG QUICKLIST-PACKED-THRESHOLD = %q", r.ToBytes())
	}
}

func TestDebugChangeReplID(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "CHANGE-REPL-ID"))
	if string(r.ToBytes()) != "+OK\r\n" {
		t.Fatalf("DEBUG CHANGE-REPL-ID = %q", r.ToBytes())
	}
}

func TestDebugNoSubcommand(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("DEBUG"))
	if !protocol.IsErrorReply(r) {
		t.Fatalf("DEBUG with no args should error, got: %q", r.ToBytes())
	}
}

func TestDebugUnknownSubcommand(t *testing.T) {
	config.Properties = &config.ServerProperties{Databases: 16}
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("DEBUG", "NOSUCH"))
	if !protocol.IsErrorReply(r) {
		t.Fatalf("DEBUG NOSUCH should error, got: %q", r.ToBytes())
	}
}
