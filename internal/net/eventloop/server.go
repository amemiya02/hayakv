package eventloop

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"golang.org/x/sys/unix"
)

const (
	readBufSize = 16 * 1024
	pollTimeout = 100 // milliseconds
)

// Server implements iface.NetServer using a single-threaded event loop
// backed by kqueue (darwin) or epoll (linux).
type Server struct {
	engine   iface.StorageEngine
	codec    iface.ProtocolCodec
	resp     iface.RespVersion
	poller   poller
	clients  map[int]*client
	listenFd int
	stopCh   chan struct{}
	blocks   *blockRegistry
}

// NewServer creates a new event loop server.
func NewServer(engine iface.StorageEngine, resp iface.RespVersion) *Server {
	return &Server{
		engine:  engine,
		resp:    resp,
		clients: make(map[int]*client),
		stopCh:  make(chan struct{}),
		blocks:  newBlockRegistry(),
	}
}

// SetCodec sets the protocol codec. Called before Run.
func (s *Server) SetCodec(codec iface.ProtocolCodec) {
	s.codec = codec
}

// Run starts the event loop server. It binds to addr, then enters the
// poll loop until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string, handler iface.NetHandler) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid addr %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid port %q: %w", portStr, err)
	}

	if err := s.listen(host, port); err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.poller, err = newPoller()
	if err != nil {
		unix.Close(s.listenFd)
		return fmt.Errorf("newPoller: %w", err)
	}

	if err := s.poller.addRead(s.listenFd); err != nil {
		s.poller.close()
		unix.Close(s.listenFd)
		return fmt.Errorf("addRead(listen): %w", err)
	}

	logger.Info(fmt.Sprintf("event loop server listening on %s:%d", host, port))

	events := make([]event, maxEvents)
	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return nil
		default:
		}

		n, err := s.poller.wait(events, pollTimeout)
		if err != nil {
			logger.Errorf("poller.wait: %v", err)
			continue
		}
		for i := 0; i < n; i++ {
			ev := events[i]
			if ev.fd == s.listenFd {
				s.accept()
				continue
			}
			c, ok := s.clients[ev.fd]
			if !ok {
				continue
			}
			if ev.readable {
				s.onReadable(c)
			}
			if ev.writable && c.bc.hasOut() {
				s.flush(c)
			}
		}
	}
}

// listen creates a non-blocking TCP socket and binds it to host:port.
func (s *Server) listen(host string, port int) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		return err
	}
	// Allow port reuse.
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		unix.Close(fd)
		return err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return err
	}

	sa := &unix.SockaddrInet4{Port: port}
	if host == "" || host == "0.0.0.0" {
		// sa.Addr is zero-valued = INADDR_ANY
	} else {
		ip := net.ParseIP(host)
		if ip == nil {
			unix.Close(fd)
			return fmt.Errorf("invalid IP %q", host)
		}
		copy(sa.Addr[:], ip.To4())
	}

	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		return err
	}
	if err := unix.Listen(fd, 128); err != nil {
		unix.Close(fd)
		return err
	}
	s.listenFd = fd
	return nil
}

// accept accepts new connections in a loop (edge-triggered style).
func (s *Server) accept() {
	for {
		nfd, sa, err := unix.Accept(s.listenFd)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				return
			}
			logger.Errorf("accept: %v", err)
			return
		}
		if err := unix.SetNonblock(nfd, true); err != nil {
			unix.Close(nfd)
			continue
		}
		remote := sockaddrString(sa)
		c := newClient(nfd, remote)
		s.clients[nfd] = c
		if err := s.poller.addRead(nfd); err != nil {
			logger.Errorf("addRead(client): %v", err)
			s.closeClient(c)
			continue
		}
		logger.Info("client connected: " + remote)
	}
}

// onReadable reads data from the client's fd, parses commands, executes
// them, and encodes replies into the client's bufConn.
func (s *Server) onReadable(c *client) {
	buf := make([]byte, readBufSize)
	for {
		n, err := unix.Read(c.fd, buf)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				break
			}
			s.closeClient(c)
			return
		}
		if n == 0 {
			// Client closed connection.
			s.closeClient(c)
			return
		}
		c.queryBuf = append(c.queryBuf, buf[:n]...)
	}

	cmds, consumed, err := parseRequests(c.queryBuf)
	if err != nil {
		// Protocol error — close the client.
		s.closeClient(c)
		return
	}
	// Keep any incomplete tail.
	c.queryBuf = c.queryBuf[consumed:]

	for _, cmdLine := range cmds {
		reply := s.engine.Exec(c.conn, cmdLine)
		if reply != nil {
			encoded := s.codec.Encode(reply, c.conn.Protocol())
			if len(encoded) > 0 {
				c.bc.Write(encoded)
			}
		}
	}

	if c.bc.hasOut() {
		s.flush(c)
	}
}

// flush writes buffered output to the client's fd.
// If not all data is written, register write-interest.
func (s *Server) flush(c *client) {
	out := c.bc.takeOut()
	if len(out) == 0 {
		return
	}
	written := 0
	for written < len(out) {
		n, err := unix.Write(c.fd, out[written:])
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				break
			}
			s.closeClient(c)
			return
		}
		written += n
	}
	if written < len(out) {
		// Put remaining data back and register write-interest.
		c.bc.out = append(out[written:], c.bc.out...)
		if !c.wantWrite {
			if err := s.poller.modReadWrite(c.fd); err != nil {
				s.closeClient(c)
				return
			}
			c.wantWrite = true
		}
	} else if c.wantWrite {
		// All data written, switch back to read-only.
		if err := s.poller.addRead(c.fd); err != nil {
			s.closeClient(c)
			return
		}
		c.wantWrite = false
	}
}

// closeClient cleans up a client and removes it from the event loop.
func (s *Server) closeClient(c *client) {
	s.engine.AfterClientClose(c.conn)
	s.poller.remove(c.fd)
	unix.Close(c.fd)
	delete(s.clients, c.fd)
	logger.Info("client disconnected: " + c.bc.RemoteAddr().String())
}

// shutdown closes all clients and releases resources.
func (s *Server) shutdown() {
	for _, c := range s.clients {
		s.closeClient(c)
	}
	if s.poller != nil {
		s.poller.close()
	}
	if s.listenFd > 0 {
		unix.Close(s.listenFd)
	}
	s.engine.Close()
}

// Close stops the server.
func (s *Server) Close() error {
	close(s.stopCh)
	return nil
}

// sockaddrString converts a unix.Sockaddr to a "host:port" string.
func sockaddrString(sa unix.Sockaddr) string {
	switch v := sa.(type) {
	case *unix.SockaddrInet4:
		ip := net.IP(v.Addr[:])
		return fmt.Sprintf("%s:%d", ip.String(), v.Port)
	case *unix.SockaddrInet6:
		ip := net.IP(v.Addr[:])
		return fmt.Sprintf("[%s]:%d", ip.String(), v.Port)
	default:
		return "unknown"
	}
}

// Ensure Server implements iface.NetServer.
var _ iface.NetServer = (*Server)(nil)

// unused suppresses lint for imported strings.
var _ = strings.TrimSpace
