package eventloop

import (
	"bytes"
	"testing"
)

// Build a RESP multibulk from args.
func buildMultibulk(args ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("*")
	buf.WriteString(itoa(len(args)))
	buf.WriteString("\r\n")
	for _, a := range args {
		buf.WriteString("$")
		buf.WriteString(itoa(len(a)))
		buf.WriteString("\r\n")
		buf.WriteString(a)
		buf.WriteString("\r\n")
	}
	return buf.Bytes()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestParseOneMultibulk(t *testing.T) {
	buf := buildMultibulk("PING")
	cmd, consumed, err := parseOneMultibulk(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if consumed != len(buf) {
		t.Fatalf("consumed = %d, want %d", consumed, len(buf))
	}
	if len(cmd) != 1 {
		t.Fatalf("len(cmd) = %d, want 1", len(cmd))
	}
	if string(cmd[0]) != "PING" {
		t.Fatalf("cmd[0] = %q, want PING", cmd[0])
	}
}

func TestParseOneMultibulkSET(t *testing.T) {
	buf := buildMultibulk("SET", "key", "value")
	cmd, consumed, err := parseOneMultibulk(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if consumed != len(buf) {
		t.Fatalf("consumed = %d, want %d", consumed, len(buf))
	}
	if len(cmd) != 3 {
		t.Fatalf("len(cmd) = %d, want 3", len(cmd))
	}
	if string(cmd[0]) != "SET" || string(cmd[1]) != "key" || string(cmd[2]) != "value" {
		t.Fatalf("cmd = %v, want [SET key value]", cmd)
	}
}

func TestParseRequestsMultiple(t *testing.T) {
	buf := append(buildMultibulk("PING"), buildMultibulk("PING")...)
	cmds, consumed, err := parseRequests(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if consumed != len(buf) {
		t.Fatalf("consumed = %d, want %d", consumed, len(buf))
	}
	if len(cmds) != 2 {
		t.Fatalf("len(cmds) = %d, want 2", len(cmds))
	}
}

func TestParseRequestsIncomplete(t *testing.T) {
	// Missing trailing \r\n after payload
	buf := []byte("*1\r\n$4\r\nPIN")
	cmds, consumed, err := parseRequests(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 0 {
		t.Fatalf("len(cmds) = %d, want 0", len(cmds))
	}
	if consumed != 0 {
		t.Fatalf("consumed = %d, want 0", consumed)
	}
}

func TestParseRequestsPartialFirstCompleteSecond(t *testing.T) {
	ping := buildMultibulk("PING")
	tail := []byte("*1\r\n$4\r\nPIN")
	buf := append(ping, tail...)
	cmds, consumed, err := parseRequests(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("len(cmds) = %d, want 1", len(cmds))
	}
	if consumed != len(ping) {
		t.Fatalf("consumed = %d, want %d", consumed, len(ping))
	}
}

func TestParseRequestsEmpty(t *testing.T) {
	cmds, consumed, err := parseRequests(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 0 || consumed != 0 {
		t.Fatalf("unexpected result: cmds=%d consumed=%d", len(cmds), consumed)
	}
}

func TestParseOneMultibulkNullBulk(t *testing.T) {
	// *2\r\n$3\r\nFOO\r\n$-1\r\n (nil bulk argument)
	var buf bytes.Buffer
	buf.WriteString("*2\r\n$3\r\nFOO\r\n$-1\r\n")
	cmd, consumed, err := parseOneMultibulk(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if consumed != buf.Len() {
		t.Fatalf("consumed = %d, want %d", consumed, buf.Len())
	}
	if cmd[1] != nil {
		t.Fatalf("cmd[1] = %v, want nil", cmd[1])
	}
}

func TestParseProtocolError(t *testing.T) {
	buf := []byte("PING\r\n")
	_, _, err := parseOneMultibulk(buf)
	if err != errProtocolError {
		t.Fatalf("err = %v, want errProtocolError", err)
	}
}
