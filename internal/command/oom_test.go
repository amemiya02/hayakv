// internal/command/oom_test.go
package database

import (
	"testing"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestDenyOOMUnderNoeviction(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "noeviction"
	s := NewStandaloneServer()
	defer s.Close()
	db := s.mustSelectDB(0)
	// fill so we are already over a tiny limit
	db.PutEntity("a", &database.DataEntity{Data: []byte("0123456789ABCDEF")})
	config.Properties.Maxmemory = 1 // over limit
	conn := connection.NewConn(nil)

	// a denyoom write (SET) must be rejected with -OOM
	reply := s.Exec(conn, [][]byte{[]byte("SET"), []byte("b"), []byte("v")})
	errReply, ok := reply.(protocol.ErrorReply)
	if !ok {
		t.Fatalf("SET over limit should be an error reply, got %T", reply)
	}
	if got := string(errReply.ToBytes()); got[:4] != "-OOM" {
		t.Fatalf("expected -OOM prefix, got %q", got)
	}

	// a read (GET) must still succeed even over the limit
	if r := s.Exec(conn, [][]byte{[]byte("GET"), []byte("a")}); r == nil {
		t.Fatalf("GET should not be blocked by OOM")
	}
	config.Properties.Maxmemory = 0 // reset for other tests
}

func TestDenyOOMAllowsWriteUnderLimit(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "noeviction"
	config.Properties.Maxmemory = 0 // unlimited
	s := NewStandaloneServer()
	defer s.Close()
	conn := connection.NewConn(nil)
	r := s.Exec(conn, [][]byte{[]byte("SET"), []byte("b"), []byte("v")})
	if er, ok := r.(protocol.ErrorReply); ok {
		t.Fatalf("SET under unlimited memory must not error: %q", er.ToBytes())
	}
}
