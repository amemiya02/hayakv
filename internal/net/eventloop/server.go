package eventloop

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

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
			if ev.readable && c.blockKeys == nil {
				s.onReadable(c)
			}
			if ev.writable && c.bc.hasOut() {
				s.flush(c)
			}
		}

		// Timeout check: expire blocked clients whose deadline has passed.
		s.expireBlockedClients()
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
		cmdName := strings.ToLower(string(cmdLine[0]))

		// Snapshot buffer offset before writing the reply, so that a
		// blocking null can be removed without clobbering earlier
		// pipeline replies (e.g. PING -> BLPOP must keep PONG).
		preLen := len(c.bc.out)

		reply := s.engine.Exec(c.conn, cmdLine)
		if reply != nil {
			encoded := s.codec.Encode(reply, c.conn.Protocol())
			if len(encoded) > 0 {
				c.bc.Write(encoded)
			}
		}

		// Blocking command integration: if BLPOP/BRPOP/XREAD/XREADGROUP
		// returned null, register the client as a waiter.
		if isBlockCommand(cmdName) && reply != nil && isNullReply(reply) {
			var keys []string
			var deadline int64

			if cmdName == "xread" || cmdName == "xreadgroup" {
				keys = extractStreamBlockKeys(cmdLine)
				deadline = extractStreamBlockDeadline(cmdLine)
			} else {
				keys = extractBlockKeys(cmdLine)
				deadline = extractBlockDeadline(cmdLine)
			}

			if len(keys) > 0 && deadline > 0 {
				// Remove only the null reply we just buffered, keeping
				// any earlier pipeline replies intact.
				c.bc.truncateTo(preLen)
				c.blockKeys = keys
				c.blockCmd = cmdName
				c.blockDeadline = deadline
				// Store the original command line for reconstruction
				// during wakeup (needed for XREAD/XREADGROUP options).
				storedCmd := make([][]byte, len(cmdLine))
				copy(storedCmd, cmdLine)
				c.blockCmdLine = storedCmd
				s.blocks.block(c, keys)
				break // stop processing further pipelined commands
			}
			// If deadline is 0, no BLOCK was specified — just send the null reply.
		}

		// After list-modifying commands, wake blocked waiters.
		if isListPushCommand(cmdName) && len(cmdLine) >= 2 {
			pushKey := string(cmdLine[1])
			s.wakeBlockedClients(pushKey)
		}
		// After stream-modifying commands, wake blocked waiters.
		if isStreamAddCommand(cmdName) && len(cmdLine) >= 2 {
			pushKey := string(cmdLine[1])
			s.wakeBlockedClients(pushKey)
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

// --- Blocking command helpers ---

func isBlockCommand(name string) bool {
	return name == "blpop" || name == "brpop" || name == "xread" || name == "xreadgroup"
}

func isStreamAddCommand(name string) bool {
	return name == "xadd"
}

func isListPushCommand(name string) bool {
	return name == "lpush" || name == "rpush"
}

func isNullReply(reply interface{}) bool {
	if reply == nil {
		return true
	}
	// Check for RESP2 null bulk ("$-1\r\n") or RESP3 null ("_\r\n")
	if b, ok := reply.(interface{ ToBytes() []byte }); ok {
		raw := b.ToBytes()
		return string(raw) == "$-1\r\n" || string(raw) == "_\r\n" || string(raw) == "*-1\r\n"
	}
	return false
}

func extractBlockKeys(cmdLine [][]byte) []string {
	// BLPOP key1 key2 ... timeout — keys are args[1..n-1], last arg is timeout
	if len(cmdLine) < 3 {
		return nil
	}
	keys := make([]string, 0, len(cmdLine)-2)
	for i := 1; i < len(cmdLine)-1; i++ {
		keys = append(keys, string(cmdLine[i]))
	}
	return keys
}

// extractBlockDeadline parses the timeout argument and returns a monotonic
// deadline in nanoseconds. Returns 0 if timeout is 0 (block indefinitely).
func extractBlockDeadline(cmdLine [][]byte) int64 {
	if len(cmdLine) < 3 {
		return 0
	}
	timeoutSec, err := strconv.ParseFloat(string(cmdLine[len(cmdLine)-1]), 64)
	if err != nil || timeoutSec <= 0 {
		return 0
	}
	return time.Now().UnixNano() + int64(timeoutSec*1e9)
}

// extractStreamBlockKeys extracts the stream keys from XREAD/XREADGROUP
// STREAMS key1 key2 ... id1 id2 ... syntax.
func extractStreamBlockKeys(cmdLine [][]byte) []string {
	streamsIdx := -1
	for i, arg := range cmdLine {
		if strings.ToUpper(string(arg)) == "STREAMS" {
			streamsIdx = i
			break
		}
	}
	if streamsIdx < 0 {
		return nil
	}
	remaining := len(cmdLine) - streamsIdx - 1
	if remaining%2 != 0 || remaining == 0 {
		return nil
	}
	keyCount := remaining / 2
	keys := make([]string, keyCount)
	for i := 0; i < keyCount; i++ {
		keys[i] = string(cmdLine[streamsIdx+1+i])
	}
	return keys
}

// extractStreamBlockDeadline parses the BLOCK timeout from XREAD/XREADGROUP
// args and returns a monotonic deadline in nanoseconds. Returns 0 if BLOCK is
// not specified or timeout is 0 (block indefinitely, handled as deadline=0
// with special semantics by the caller).
func extractStreamBlockDeadline(cmdLine [][]byte) int64 {
	for i, arg := range cmdLine {
		if strings.ToUpper(string(arg)) == "BLOCK" && i+1 < len(cmdLine) {
			timeoutMs, err := strconv.ParseInt(string(cmdLine[i+1]), 10, 64)
			if err != nil || timeoutMs < 0 {
				return 0
			}
			if timeoutMs == 0 {
				// Block indefinitely: use a far-future deadline.
				return time.Now().UnixNano() + 3600*24*365*1e9 // 1 year
			}
			return time.Now().UnixNano() + timeoutMs*1e6
		}
	}
	return 0
}

// wakeBlockedClients checks if any client is blocked on pushKey and retries
// their blocking command. For BLPOP/BRPOP, if the list now has elements the
// client gets a reply and is unblocked. For XREAD/XREADGROUP, if new stream
// entries are available the client gets a reply and is unblocked.
func (s *Server) wakeBlockedClients(pushKey string) {
	waiters := s.blocks.waiters(pushKey)
	if len(waiters) == 0 {
		return
	}
	// Copy the list since unblock modifies it.
	pending := make([]*client, len(waiters))
	copy(pending, waiters)

	for _, c := range pending {
		var blockCmd [][]byte

		if c.blockCmd == "xread" || c.blockCmd == "xreadgroup" {
			// Reconstruct the XREAD/XREADGROUP command for wakeup.
			// We reuse the original command line but replace stream IDs
			// with "0-0" so that any entry present is returned.
			blockCmd = s.buildStreamWakeupCmd(c)
		} else {
			// BLPOP/BRPOP: re-issue with timeout 0 (immediate check).
			blockCmd = make([][]byte, 0, len(c.blockKeys)+2)
			blockCmd = append(blockCmd, []byte(c.blockCmd))
			for _, k := range c.blockKeys {
				blockCmd = append(blockCmd, []byte(k))
			}
			blockCmd = append(blockCmd, []byte("0"))
		}

		reply := s.engine.Exec(c.conn, blockCmd)
		if reply != nil && !isNullReply(reply) {
			// Got a result — send reply and unblock.
			encoded := s.codec.Encode(reply, c.conn.Protocol())
			if len(encoded) > 0 {
				c.bc.Write(encoded)
			}
			s.blocks.unblock(c)
			c.blockKeys = nil
			c.blockCmd = ""
			c.blockCmdLine = nil
			s.flush(c)
		}
		// If still null, leave the client blocked.
	}
}

// buildStreamWakeupCmd reconstructs an XREAD/XREADGROUP command for wakeup
// checks. It strips BLOCK and replaces stream IDs with "0-0" so any existing
// entries are returned.
func (s *Server) buildStreamWakeupCmd(c *client) [][]byte {
	if len(c.blockCmdLine) > 0 {
		// Rebuild from original command line, stripping BLOCK and replacing IDs.
		orig := c.blockCmdLine
		result := make([][]byte, 0, len(orig))
		result = append(result, orig[0]) // XREAD or XREADGROUP

		// Copy args before STREAMS, skipping BLOCK <timeout>
		i := 1
		for i < len(orig) {
			upper := strings.ToUpper(string(orig[i]))
			if upper == "STREAMS" {
				break
			}
			if upper == "BLOCK" {
				i += 2 // skip BLOCK and its timeout value
				continue
			}
			result = append(result, orig[i])
			i++
		}

		if i < len(orig) && strings.ToUpper(string(orig[i])) == "STREAMS" {
			result = append(result, orig[i]) // STREAMS keyword
			i++
			remaining := len(orig) - i
			if remaining > 0 && remaining%2 == 0 {
				keyCount := remaining / 2
				// Add keys
				for j := 0; j < keyCount; j++ {
					result = append(result, orig[i+j])
				}
				// Add "0-0" for each key (replaces original IDs)
				for j := 0; j < keyCount; j++ {
					result = append(result, []byte("0-0"))
				}
				return result
			}
		}
	}

	// Fallback: minimal reconstruction from blockKeys.
	result := make([][]byte, 0, len(c.blockKeys)*2+3)
	result = append(result, []byte(c.blockCmd))
	if c.blockCmd == "xreadgroup" {
		// Cannot reconstruct without group/consumer info; this path
		// should not be reached when blockCmdLine is stored.
		result = append(result, []byte("GROUP"), []byte("?"), []byte("?"))
	}
	result = append(result, []byte("STREAMS"))
	for _, k := range c.blockKeys {
		result = append(result, []byte(k))
	}
	for range c.blockKeys {
		result = append(result, []byte("0-0"))
	}
	return result
}

// expireBlockedClients scans all clients and unblocks any whose timeout has
// expired, sending a null multi-bulk reply (identical to Redis timeout behavior).
func (s *Server) expireBlockedClients() {
	now := time.Now().UnixNano()
	for _, c := range s.clients {
		if c.blockKeys == nil || c.blockDeadline == 0 {
			continue
		}
		if now >= c.blockDeadline {
			// Timeout expired — send null multi-bulk reply
			nullReply := s.codec.Encode(&protocolNullMultiBulk{}, c.conn.Protocol())
			if len(nullReply) > 0 {
				c.bc.Write(nullReply)
			}
			s.blocks.unblock(c)
			c.blockKeys = nil
			c.blockCmd = ""
			c.blockCmdLine = nil
			c.blockDeadline = 0
			s.flush(c)
		}
	}
}

// protocolNullMultiBulk represents a RESP null array (*-1\r\n), used as the
// timeout reply for BLPOP/BRPOP.
type protocolNullMultiBulk struct{}

func (r *protocolNullMultiBulk) ToBytes() []byte {
	return []byte("*-1\r\n")
}
