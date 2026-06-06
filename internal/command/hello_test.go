package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestHelloSwitchesProtocolAndReplies(t *testing.T) {
	server := NewStandaloneServer()
	conn := connection.NewConn(nil)

	reply := server.Exec(conn, [][]byte{[]byte("HELLO"), []byte("3")})
	if out := string(reply.ToBytes()); !strings.HasPrefix(out, "%7\r\n") {
		t.Fatalf("HELLO 3 should be a 7-field map, got %q", out)
	}
	if conn.Protocol() != 3 {
		t.Fatalf("HELLO 3 did not switch to RESP3 (got %d)", conn.Protocol())
	}
	bad := server.Exec(conn, [][]byte{[]byte("HELLO"), []byte("4")})
	if !strings.HasPrefix(string(bad.ToBytes()), "-NOPROTO") {
		t.Fatalf("HELLO 4 should be NOPROTO, got %q", bad.ToBytes())
	}
}
