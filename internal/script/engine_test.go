package script

import (
	"crypto/sha1"
	"encoding/hex"
	"testing"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func stubInvoker(_ iredis.Connection, cmdLine [][]byte) iredis.Reply {
	switch string(cmdLine[0]) {
	case "GET":
		return protocol.MakeBulkReply([]byte("stub-value"))
	case "SET":
		return protocol.MakeOkReply()
	case "ECHO":
		if len(cmdLine) > 1 {
			return protocol.MakeBulkReply(cmdLine[1])
		}
		return protocol.MakeNullBulkReply()
	default:
		return protocol.MakeOkReply()
	}
}

func TestEvalKeysArgvAndCall(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)

	t.Run("redis.call GET", func(t *testing.T) {
		got := string(e.Eval(nil, "return redis.call('GET', KEYS[1])", []string{"k1"}, []string{"a1"}, false).ToBytes())
		if got != "$10\r\nstub-value\r\n" {
			t.Fatalf("eval GET = %q", got)
		}
	})

	t.Run("KEYS and ARGV", func(t *testing.T) {
		got := string(e.Eval(nil, "return {KEYS[1], ARGV[1]}", []string{"k1"}, []string{"a1"}, false).ToBytes())
		if got != "*2\r\n$2\r\nk1\r\n$2\r\na1\r\n" {
			t.Fatalf("eval KEYS/ARGV = %q", got)
		}
	})

	t.Run("return number", func(t *testing.T) {
		got := string(e.Eval(nil, "return 42", nil, nil, false).ToBytes())
		if got != ":42\r\n" {
			t.Fatalf("eval 42 = %q", got)
		}
	})

	t.Run("return string", func(t *testing.T) {
		got := string(e.Eval(nil, "return 'hello'", nil, nil, false).ToBytes())
		if got != "$5\r\nhello\r\n" {
			t.Fatalf("eval hello = %q", got)
		}
	})

	t.Run("return nil", func(t *testing.T) {
		got := string(e.Eval(nil, "return nil", nil, nil, false).ToBytes())
		if got != "$-1\r\n" {
			t.Fatalf("eval nil = %q", got)
		}
	})

	t.Run("return true", func(t *testing.T) {
		got := string(e.Eval(nil, "return true", nil, nil, false).ToBytes())
		if got != ":1\r\n" {
			t.Fatalf("eval true = %q", got)
		}
	})

	t.Run("return false", func(t *testing.T) {
		got := string(e.Eval(nil, "return false", nil, nil, false).ToBytes())
		if got != "$-1\r\n" {
			t.Fatalf("eval false = %q", got)
		}
	})
}

func TestSha1HexAndCache(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	body := "return 1"
	s := sha1.Sum([]byte(body))
	want := hex.EncodeToString(s[:])

	sha := e.Load(body)
	if sha != want {
		t.Fatalf("Load = %q want %q", sha, want)
	}

	ok := e.Exists([]string{want, "deadbeef"})
	if !ok[0] || ok[1] {
		t.Fatalf("Exists = %v", ok)
	}

	got := string(e.EvalSha(nil, want, nil, nil, false).ToBytes())
	if got != ":1\r\n" {
		t.Fatalf("EvalSha = %q", got)
	}
}

func TestEvalShaMissing(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	got := string(e.EvalSha(nil, "nonexistent", nil, nil, false).ToBytes())
	if got != "-NOSCRIPT No matching script. Please use EVAL.\r\n" {
		t.Fatalf("EvalSha missing = %q", got)
	}
}

func TestFlush(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	sha := e.Load("return 1")
	if !e.Exists([]string{sha})[0] {
		t.Fatal("expected script to exist before flush")
	}
	e.Flush()
	if e.Exists([]string{sha})[0] {
		t.Fatal("expected script to not exist after flush")
	}
}

func TestPcallProtected(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	// pcall should not raise an error; it should return the error as a value
	got := e.Eval(nil, `return redis.pcall("NONEXIST")`, nil, nil, false)
	b := got.ToBytes()
	if len(b) == 0 || b[0] == '-' {
		t.Fatalf("pcall should not produce a top-level error reply, got %q", b)
	}
}

func TestCallErrorRaises(t *testing.T) {
	// An invoker that always returns an error
	errInvoker := func(_ iredis.Connection, cmdLine [][]byte) iredis.Reply {
		return protocol.MakeErrReply("ERR no such command")
	}
	e := NewEngine(errInvoker, 5000)
	got := e.Eval(nil, `return redis.call("FOO")`, nil, nil, false)
	b := string(got.ToBytes())
	if len(b) == 0 || b[0] != '-' {
		t.Fatalf("call with error invoker should return error reply, got %q", b)
	}
	if !contains(b, "no such command") {
		t.Fatalf("expected error message in reply, got %q", b)
	}
}

func TestErrorReplyTable(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	got := string(e.Eval(nil, `return redis.error_reply("boom")`, nil, nil, false).ToBytes())
	if got != "-boom\r\n" {
		t.Fatalf("error_reply = %q", got)
	}
}

func TestStatusReplyTable(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	got := string(e.Eval(nil, `return redis.status_reply("ok-something")`, nil, nil, false).ToBytes())
	if got != "+ok-something\r\n" {
		t.Fatalf("status_reply = %q", got)
	}
}

func TestSha1HexLua(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	got := string(e.Eval(nil, `return redis.sha1hex("hello")`, nil, nil, false).ToBytes())
	// sha1("hello") = aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
	if got != "$40\r\naaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d\r\n" {
		t.Fatalf("sha1hex = %q", got)
	}
}

func TestLuaSyntaxError(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)
	got := string(e.Eval(nil, `this is not valid lua`, nil, nil, false).ToBytes())
	if len(got) == 0 || got[0] != '-' {
		t.Fatalf("expected error reply for syntax error, got %q", got)
	}
	if !contains(got, "Error running script") {
		t.Fatalf("expected 'Error running script' in reply, got %q", got)
	}
}

func TestEvalReadOnlyRejectsWrite(t *testing.T) {
	e := NewEngine(stubInvoker, 5000)

	t.Run("call SET rejected", func(t *testing.T) {
		got := string(e.Eval(nil, `return redis.call("SET", "k", "v")`, nil, nil, true).ToBytes())
		if len(got) == 0 || got[0] != '-' {
			t.Fatalf("expected error reply, got %q", got)
		}
		if !contains(got, "read-only") {
			t.Fatalf("expected 'read-only' in error, got %q", got)
		}
	})

	t.Run("pcall SET returns error as value", func(t *testing.T) {
		got := e.Eval(nil, `return redis.pcall("SET", "k", "v")`, nil, nil, true)
		b := got.ToBytes()
		// pcall should not be a top-level error
		if len(b) > 0 && b[0] == '-' {
			t.Fatalf("pcall should not produce top-level error, got %q", b)
		}
		// but the returned value should be an error table
		s := string(b)
		if !contains(s, "read-only") {
			t.Fatalf("expected 'read-only' in pcall result, got %q", s)
		}
	})

	t.Run("call GET allowed", func(t *testing.T) {
		got := string(e.Eval(nil, `return redis.call("GET", "k")`, nil, nil, true).ToBytes())
		// GET should work fine (returns stub-value from stubInvoker)
		if got != "$10\r\nstub-value\r\n" {
			t.Fatalf("GET in readonly mode = %q", got)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
