package eventloop

import (
	"github.com/amemiya02/hayakv/internal/server/connection"
)

// client holds the state for a single connected client in the event loop.
type client struct {
	fd            int
	queryBuf      []byte
	bc            *bufConn
	conn          *connection.Connection
	wantWrite     bool
	blockKeys     []string // keys this client is blocked on (BLPOP/BRPOP), nil if not blocked
	blockCmd      string   // original command name: "blpop" or "brpop"
	blockDeadline int64    // monotonic deadline in nanoseconds; 0 means no timeout
}

// newClient creates a client for the given fd and remote address.
func newClient(fd int, remote string) *client {
	bc := newBufConn(remote)
	conn := connection.NewConn(bc)
	return &client{
		fd:       fd,
		queryBuf: make([]byte, 0, 1024),
		bc:       bc,
		conn:     conn,
	}
}
