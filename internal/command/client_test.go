package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestClientSetGetName(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()

	// Set name
	r := s.Exec(c, utils.ToCmdLine("CLIENT", "SETNAME", "my-client"))
	asserts.AssertStatusReply(t, r, "OK")

	// Get name
	r = s.Exec(c, utils.ToCmdLine("CLIENT", "GETNAME"))
	asserts.AssertBulkReply(t, r, "my-client")
}

func TestClientGetNilName(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()

	r := s.Exec(c, utils.ToCmdLine("CLIENT", "GETNAME"))
	if string(r.ToBytes()) != "$-1\r\n" {
		t.Fatalf("GETNAME (nil): %q", r.ToBytes())
	}
}

func TestClientID(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()
	r := s.Exec(c, utils.ToCmdLine("CLIENT", "ID"))
	if r.ToBytes()[0] != ':' {
		t.Fatalf("ID should be integer: %q", r.ToBytes())
	}
}

func TestClientInfo(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()
	r := s.Exec(c, utils.ToCmdLine("CLIENT", "INFO"))
	info := string(r.ToBytes())
	if !strings.Contains(info, "id=") || !strings.Contains(info, "addr=") {
		t.Fatalf("INFO missing fields: %q", info)
	}
}

func TestClientInfoWithSetName(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()
	s.Exec(c, utils.ToCmdLine("CLIENT", "SETNAME", "test-client"))
	r := s.Exec(c, utils.ToCmdLine("CLIENT", "INFO"))
	info := string(r.ToBytes())
	if !strings.Contains(info, "name=test-client") {
		t.Fatalf("INFO missing name: %q", info)
	}
}

func TestClientSetInfo(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()

	r := s.Exec(c, utils.ToCmdLine("CLIENT", "SETINFO", "lib-name", "mylib"))
	asserts.AssertStatusReply(t, r, "OK")

	r = s.Exec(c, utils.ToCmdLine("CLIENT", "SETINFO", "lib-ver", "1.0.0"))
	asserts.AssertStatusReply(t, r, "OK")

	r = s.Exec(c, utils.ToCmdLine("CLIENT", "INFO"))
	info := string(r.ToBytes())
	if !strings.Contains(info, "lib-name=mylib") {
		t.Fatalf("INFO missing lib-name: %q", info)
	}
	if !strings.Contains(info, "lib-ver=1.0.0") {
		t.Fatalf("INFO missing lib-ver: %q", info)
	}
}

func TestClientList(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c1 := connection.NewFakeConn()
	c2 := connection.NewFakeConn()

	s.Exec(c1, utils.ToCmdLine("CLIENT", "SETNAME", "client-1"))
	s.Exec(c2, utils.ToCmdLine("CLIENT", "SETNAME", "client-2"))

	r := s.Exec(c1, utils.ToCmdLine("CLIENT", "LIST"))
	list := string(r.ToBytes())
	if !strings.Contains(list, "name=client-1") {
		t.Fatalf("LIST missing client-1: %q", list)
	}
	if !strings.Contains(list, "name=client-2") {
		t.Fatalf("LIST missing client-2: %q", list)
	}
}

func TestClientReply(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()

	r := s.Exec(c, utils.ToCmdLine("CLIENT", "REPLY", "OFF"))
	asserts.AssertStatusReply(t, r, "OK")

	r = s.Exec(c, utils.ToCmdLine("CLIENT", "REPLY", "ON"))
	asserts.AssertStatusReply(t, r, "OK")

	r = s.Exec(c, utils.ToCmdLine("CLIENT", "REPLY", "SKIP"))
	asserts.AssertStatusReply(t, r, "OK")
}

func TestClientUnknownSubcommand(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()

	r := s.Exec(c, utils.ToCmdLine("CLIENT", "FOOBAR"))
	asserts.AssertErrReply(t, r, "ERR unknown subcommand 'FOOBAR'")
}

func TestClientNoArgs(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()

	r := s.Exec(c, utils.ToCmdLine("CLIENT"))
	asserts.AssertErrReply(t, r, "ERR wrong number of arguments for 'client' command")
}
