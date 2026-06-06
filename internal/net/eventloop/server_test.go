package eventloop

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// fakeEngine is a minimal StorageEngine that responds to PING and ECHO.
type fakeEngine struct{}

func (e *fakeEngine) Exec(client iredis.Connection, cmdLine iface.CmdLine) iredis.Reply {
	cmd := strings.ToUpper(string(cmdLine[0]))
	switch cmd {
	case "PING":
		return protocol.MakeStatusReply("PONG")
	case "ECHO":
		if len(cmdLine) > 1 {
			return protocol.MakeBulkReply(cmdLine[1])
		}
		return protocol.MakeErrReply("wrong number of arguments for 'echo' command")
	default:
		return protocol.MakeErrReply("ERR unknown command '" + string(cmdLine[0]) + "'")
	}
}

func (e *fakeEngine) AfterClientClose(client iredis.Connection) {}
func (e *fakeEngine) Close()                                     {}

// fakeCodec implements iface.ProtocolCodec for tests.
// It uses resp2 encoding (reply.ToBytes()).
type fakeCodec struct{}

func (c *fakeCodec) Encode(reply iredis.Reply, resp iredis.RespVersion) []byte {
	return reply.ToBytes()
}

func (c *fakeCodec) DecodeStream(reader io.Reader) <-chan iface.ProtocolPayload {
	return nil
}

func (c *fakeCodec) DecodeOne(data []byte) (iredis.Reply, error) {
	return nil, nil
}

func TestServerPingPong(t *testing.T) {
	engine := &fakeEngine{}
	srv := NewServer(engine, iredis.RESP2)
	srv.codec = &fakeCodec{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	go func() {
		srv.Run(ctx, addr, nil)
	}()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(buf[:n])
	if got != "+PONG\r\n" {
		t.Fatalf("response = %q, want +PONG\\r\\n", got)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestServerECHO(t *testing.T) {
	engine := &fakeEngine{}
	srv := NewServer(engine, iredis.RESP2)
	srv.codec = &fakeCodec{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	go func() {
		srv.Run(ctx, addr, nil)
	}()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("*2\r\n$4\r\nECHO\r\n$5\r\nhello\r\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(buf[:n])
	want := "$5\r\nhello\r\n"
	if got != want {
		t.Fatalf("response = %q, want %q", got, want)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestServerMultipleCommands(t *testing.T) {
	engine := &fakeEngine{}
	srv := NewServer(engine, iredis.RESP2)
	srv.codec = &fakeCodec{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	go func() {
		srv.Run(ctx, addr, nil)
	}()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(buf[:n])
	want := "+PONG\r\n+PONG\r\n"
	if got != want {
		t.Fatalf("response = %q, want %q", got, want)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}
