package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestEvalSetGetViaScript(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("EVAL", "redis.call('set',KEYS[1],ARGV[1]) return redis.call('get',KEYS[1])", "1", "k", "v"))
	asserts.AssertBulkReply(t, r, "v")

	// verify script write is visible to normal GET
	r2 := s.Exec(conn, utils.ToCmdLine("GET", "k"))
	asserts.AssertBulkReply(t, r2, "v")
}

func TestEvalReturnNumber(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("EVAL", "return 42", "0"))
	if string(r.ToBytes()) != ":42\r\n" {
		t.Fatalf("EVAL return 42 = %q", r.ToBytes())
	}
}

func TestEvalReturnNil(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("EVAL", "return nil", "0"))
	if string(r.ToBytes()) != "$-1\r\n" {
		t.Fatalf("EVAL return nil = %q", r.ToBytes())
	}
}

func TestEvalSyntaxError(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("EVAL", "invalid lua!!", "0"))
	if !strings.HasPrefix(string(r.ToBytes()), "-") {
		t.Fatalf("EVAL syntax error should return error, got = %q", r.ToBytes())
	}
}

func TestEvalArgErrors(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// too few args
	r := s.Exec(conn, utils.ToCmdLine("EVAL"))
	asserts.AssertErrReply(t, r, "ERR wrong number of arguments for 'eval' command")

	// missing numkeys
	r = s.Exec(conn, utils.ToCmdLine("EVAL", "return 1"))
	asserts.AssertErrReply(t, r, "ERR wrong number of arguments for 'eval' command")

	// numkeys not integer
	r = s.Exec(conn, utils.ToCmdLine("EVAL", "return 1", "abc"))
	asserts.AssertErrReply(t, r, "ERR value is not an integer or out of range")

	// numkeys > args
	r = s.Exec(conn, utils.ToCmdLine("EVAL", "return 1", "5", "a", "b"))
	asserts.AssertErrReply(t, r, "ERR Number of keys can't be greater than number of args")
}

func TestEvalShaNoScript(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	r := s.Exec(conn, utils.ToCmdLine("EVALSHA", strings.Repeat("0", 40), "0"))
	if !strings.HasPrefix(string(r.ToBytes()), "-NOSCRIPT") {
		t.Fatalf("EVALSHA unknown = %q", r.ToBytes())
	}
}

func TestEvalShaAfterLoad(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// load script
	r := s.Exec(conn, utils.ToCmdLine("SCRIPT", "LOAD", "return 1"))
	sha := strings.TrimSpace(string(r.ToBytes()))
	// strip bulk string prefix: $<len>\r\n
	if idx := strings.Index(sha, "\r\n"); idx >= 0 {
		sha = sha[idx+2:]
	}
	if idx := strings.Index(sha, "\r\n"); idx >= 0 {
		sha = sha[:idx]
	}

	// evalsha should work
	r2 := s.Exec(conn, utils.ToCmdLine("EVALSHA", sha, "0"))
	if string(r2.ToBytes()) != ":1\r\n" {
		t.Fatalf("EVALSHA after LOAD = %q", r2.ToBytes())
	}
}

func TestScriptLoadExistsFlush(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// load
	r := s.Exec(conn, utils.ToCmdLine("SCRIPT", "LOAD", "return 1"))
	sha := strings.TrimSpace(string(r.ToBytes()))
	if idx := strings.Index(sha, "\r\n"); idx >= 0 {
		sha = sha[idx+2:]
	}
	if idx := strings.Index(sha, "\r\n"); idx >= 0 {
		sha = sha[:idx]
	}
	if len(sha) != 40 {
		t.Fatalf("SCRIPT LOAD sha length = %d, want 40: %q", len(sha), sha)
	}

	// exists: one real, one fake
	r2 := s.Exec(conn, utils.ToCmdLine("SCRIPT", "EXISTS", sha, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
	if !strings.Contains(string(r2.ToBytes()), ":1\r\n") {
		t.Fatalf("SCRIPT EXISTS = %q", r2.ToBytes())
	}

	// flush
	r3 := s.Exec(conn, utils.ToCmdLine("SCRIPT", "FLUSH"))
	if string(r3.ToBytes()) != "+OK\r\n" {
		t.Fatalf("SCRIPT FLUSH = %q", r3.ToBytes())
	}

	// after flush, exists should return false
	r4 := s.Exec(conn, utils.ToCmdLine("SCRIPT", "EXISTS", sha))
	if !strings.Contains(string(r4.ToBytes()), ":0\r\n") {
		t.Fatalf("SCRIPT EXISTS after FLUSH = %q", r4.ToBytes())
	}
}

func TestScriptArgErrors(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// no subcommand
	r := s.Exec(conn, utils.ToCmdLine("SCRIPT"))
	asserts.AssertErrReply(t, r, "ERR wrong number of arguments for 'script' command")

	// unknown subcommand
	r = s.Exec(conn, utils.ToCmdLine("SCRIPT", "FOOBAR"))
	asserts.AssertErrReply(t, r, "ERR Unknown SCRIPT subcommand")

	// load with wrong arity
	r = s.Exec(conn, utils.ToCmdLine("SCRIPT", "LOAD"))
	asserts.AssertErrReply(t, r, "ERR wrong number of arguments for 'script|load' command")
}

func TestScriptKill(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()
	// Kill sets a flag in the engine; it always succeeds in the gopher-lua implementation.
	r := s.Exec(conn, utils.ToCmdLine("SCRIPT", "KILL"))
	if string(r.ToBytes()) != "+OK\r\n" {
		t.Fatalf("SCRIPT KILL = %q", r.ToBytes())
	}
}

func TestEvalRoRejectsWrite(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// EVAL_RO calling SET must fail
	r := s.Exec(conn, utils.ToCmdLine("EVAL_RO", "redis.call('set',KEYS[1],ARGV[1]) return 'ok'", "1", "k", "v"))
	b := string(r.ToBytes())
	if len(b) == 0 || b[0] != '-' {
		t.Fatalf("EVAL_RO with SET should return error, got %q", b)
	}
	if !strings.Contains(b, "read-only") {
		t.Fatalf("expected 'read-only' in error, got %q", b)
	}

	// EVAL_RO calling GET is fine
	r2 := s.Exec(conn, utils.ToCmdLine("EVAL_RO", "return redis.call('get','nosuchkey')", "0"))
	b2 := string(r2.ToBytes())
	if b2 != "$-1\r\n" {
		t.Fatalf("EVAL_RO with GET should return nil, got %q", b2)
	}

	// Normal EVAL with SET succeeds (not read-only)
	r3 := s.Exec(conn, utils.ToCmdLine("EVAL", "redis.call('set',KEYS[1],ARGV[1]) return redis.call('get',KEYS[1])", "1", "rwkey", "rwval"))
	asserts.AssertBulkReply(t, r3, "rwval")
}

func TestEvalShaRoRejectsWrite(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewFakeConn()

	// Load a write script
	r := s.Exec(conn, utils.ToCmdLine("SCRIPT", "LOAD", "redis.call('set','k','v') return 'ok'"))
	sha := strings.TrimSpace(string(r.ToBytes()))
	if idx := strings.Index(sha, "\r\n"); idx >= 0 {
		sha = sha[idx+2:]
	}
	if idx := strings.Index(sha, "\r\n"); idx >= 0 {
		sha = sha[:idx]
	}

	// EVALSHA_RO with the write script must fail
	r2 := s.Exec(conn, utils.ToCmdLine("EVALSHA_RO", sha, "0"))
	b := string(r2.ToBytes())
	if len(b) == 0 || b[0] != '-' {
		t.Fatalf("EVALSHA_RO with write script should return error, got %q", b)
	}
	if !strings.Contains(b, "read-only") {
		t.Fatalf("expected 'read-only' in error, got %q", b)
	}
}
