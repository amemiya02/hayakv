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
