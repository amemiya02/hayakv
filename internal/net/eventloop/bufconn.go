package eventloop

import (
	"net"
	"time"
)

// bufConn is a net.Conn that buffers writes in memory instead of
// writing directly to the socket. The event loop flushes the buffer
// to the real fd when it becomes writable.
type bufConn struct {
	out    []byte
	remote bufAddr
}

type bufAddr struct {
	addr string
}

func (a bufAddr) Network() string { return "tcp" }
func (a bufAddr) String() string  { return a.addr }

// newBufConn creates a bufConn for the given remote address.
func newBufConn(remote string) *bufConn {
	return &bufConn{remote: bufAddr{addr: remote}}
}

// Write appends b to the output buffer. It always succeeds (returns len(b), nil).
func (c *bufConn) Write(b []byte) (int, error) {
	c.out = append(c.out, b...)
	return len(b), nil
}

// hasOut returns true if there is data waiting to be flushed.
func (c *bufConn) hasOut() bool {
	return len(c.out) > 0
}

// takeOut returns the buffered output and resets the buffer.
func (c *bufConn) takeOut() []byte {
	out := c.out
	c.out = nil
	return out
}

// Read is a no-op — reading is done directly on the fd.
func (c *bufConn) Read(b []byte) (int, error)         { return 0, nil }
func (c *bufConn) Close() error                       { return nil }
func (c *bufConn) LocalAddr() net.Addr                { return c.remote }
func (c *bufConn) RemoteAddr() net.Addr               { return c.remote }
func (c *bufConn) SetDeadline(t time.Time) error      { return nil }
func (c *bufConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *bufConn) SetWriteDeadline(t time.Time) error { return nil }
