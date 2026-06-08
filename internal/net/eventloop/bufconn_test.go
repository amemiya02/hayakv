package eventloop

import (
	"testing"
)

func TestBufConnWriteAndTake(t *testing.T) {
	bc := newBufConn("127.0.0.1:6379")
	if bc.hasOut() {
		t.Fatal("expected no output initially")
	}
	n, err := bc.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write: n=%d err=%v", n, err)
	}
	if !bc.hasOut() {
		t.Fatal("expected output after Write")
	}
	out := bc.takeOut()
	if string(out) != "hello" {
		t.Fatalf("takeOut = %q, want hello", out)
	}
	if bc.hasOut() {
		t.Fatal("expected no output after takeOut")
	}
}

func TestBufConnMultipleWrites(t *testing.T) {
	bc := newBufConn("10.0.0.1:1234")
	bc.Write([]byte("abc"))
	bc.Write([]byte("def"))
	out := bc.takeOut()
	if string(out) != "abcdef" {
		t.Fatalf("takeOut = %q, want abcdef", out)
	}
}

func TestBufConnRemoteAddr(t *testing.T) {
	bc := newBufConn("192.168.1.1:9999")
	if bc.RemoteAddr().String() != "192.168.1.1:9999" {
		t.Fatalf("RemoteAddr = %q", bc.RemoteAddr())
	}
}

func TestBufConnWriteAndTakeOut(t *testing.T) {
	bc := newBufConn("127.0.0.1:12345")

	// Write 100 replies
	for i := 0; i < 100; i++ {
		bc.Write([]byte("+OK\r\n"))
	}

	out := bc.takeOut()
	if len(out) != 100*5 {
		t.Fatalf("expected %d bytes, got %d", 100*5, len(out))
	}

	// After takeOut, buffer should be empty but reusable.
	if bc.hasOut() {
		t.Fatal("buffer should be empty after takeOut")
	}

	// Write more data — should reuse backing array.
	bc.Write([]byte("$-1\r\n"))
	if !bc.hasOut() {
		t.Fatal("buffer should have data after new write")
	}
}

func TestBufConnTruncateTo(t *testing.T) {
	bc := newBufConn("127.0.0.1:12345")

	// Simulate pipeline: PING + BLPOP null.
	bc.Write([]byte("+PONG\r\n")) // 7 bytes
	bc.Write([]byte("$-1\r\n"))   // 5 bytes, total 12
	pongLen := 7

	bc.truncateTo(pongLen)

	out := bc.takeOut()
	if string(out) != "+PONG\r\n" {
		t.Fatalf("expected PONG, got %q", out)
	}
}

func TestBufConnTruncateToZero(t *testing.T) {
	bc := newBufConn("127.0.0.1:12345")
	bc.Write([]byte("$-1\r\n"))
	bc.truncateTo(0)
	if bc.hasOut() {
		t.Fatal("buffer should be empty after truncateTo(0)")
	}
}
