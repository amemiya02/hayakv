package connection

import (
	"io"
	"sync"
)

// FakeConn implements redis.Connection for test. It behaves like an
// in-memory pipe: Write appends to a buffer, Read blocks until data
// arrives or the connection is closed. All state is guarded by mu so
// reader and writer may run on different goroutines.
type FakeConn struct {
	Connection
	buf    []byte
	offset int
	waitOn chan struct{}
	closed bool
	mu     sync.Mutex
}

func NewFakeConn() *FakeConn {
	c := &FakeConn{}
	return c
}

// Write writes data to buffer
func (c *FakeConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, io.EOF
	}
	c.buf = append(c.buf, b...)
	c.notifyLocked()
	return len(b), nil
}

// notifyLocked wakes up a blocked Read. Callers must hold c.mu.
func (c *FakeConn) notifyLocked() {
	if c.waitOn != nil {
		close(c.waitOn)
		c.waitOn = nil
	}
}

// Read reads data from buffer, blocking until data arrives or the
// connection is closed
func (c *FakeConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		n := copy(p, c.buf[c.offset:])
		c.offset += n
		if n > 0 {
			if c.closed {
				return n, io.EOF
			}
			return n, nil
		}
		if c.closed {
			return 0, io.EOF
		}
		if c.waitOn == nil {
			c.waitOn = make(chan struct{})
		}
		waitOn := c.waitOn
		c.mu.Unlock()
		<-waitOn
		c.mu.Lock()
	}
}

// Clean resets the buffer
func (c *FakeConn) Clean() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifyLocked()
	c.buf = nil
	c.offset = 0
}

// Bytes returns a copy of the written data
func (c *FakeConn) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]byte, len(c.buf))
	copy(out, c.buf)
	return out
}

func (c *FakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.notifyLocked()
	return nil
}

func (c *FakeConn) RemoteAddr() string {
	return ""
}
