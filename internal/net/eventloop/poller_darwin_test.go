//go:build darwin

package eventloop

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestKqueuePollerCreateClose(t *testing.T) {
	p, err := newPoller()
	if err != nil {
		t.Fatalf("newPoller: %v", err)
	}
	if err := p.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestKqueuePollerPipe(t *testing.T) {
	p, err := newPoller()
	if err != nil {
		t.Fatalf("newPoller: %v", err)
	}
	defer p.close()

	var fds [2]int
	err = unix.Pipe(fds[:])
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() {
		unix.Close(fds[0])
		unix.Close(fds[1])
	}()

	// Set read end non-blocking
	if err := unix.SetNonblock(fds[0], true); err != nil {
		t.Fatalf("SetNonblock: %v", err)
	}

	if err := p.addRead(fds[0]); err != nil {
		t.Fatalf("addRead: %v", err)
	}

	// Write a byte so the read end becomes readable
	buf := []byte{1}
	_, err = unix.Write(fds[1], buf)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	events := make([]event, 8)
	n, err := p.wait(events, 100)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d, want 1", n)
	}
	if events[0].fd != fds[0] {
		t.Fatalf("fd = %d, want %d", events[0].fd, fds[0])
	}
	if !events[0].readable {
		t.Fatal("expected readable")
	}
}
