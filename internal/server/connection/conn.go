package connection

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/sync/wait"
)

const (
	// flagSlave means this a connection with slave
	flagSlave = uint64(1 << iota)
	// flagSlave means this a connection with master
	flagMaster
	// flagMulti means this connection is within a transaction
	flagMulti
)

// Connection represents a connection with a redis-cli
type Connection struct {
	conn net.Conn

	// wait until finish sending data, used for graceful shutdown
	sendingData wait.Wait

	// lock while server sending response
	mu    sync.Mutex
	flags uint64

	// subscribing channels
	subs map[string]bool

	// pattern subscriptions
	pSubs map[string]bool

	// password may be changed by CONFIG command during runtime, so store the password
	password string

	// queued commands for `multi`
	queue    [][][]byte
	watching map[string]uint32
	txErrors []error

	// selected db
	selectedDB int

	// protocol is the negotiated RESP version (RESP2 until HELLO 3)
	protocol redis.RespVersion

	// closed is an atomic flag ensuring Close() is idempotent — the reset
	// + pool return runs exactly once even when Handler.Close and the
	// per-conn Handle loop race on the same Connection.
	closed int32

	// onClose is called once inside the CAS guard, before fields are reset.
	// Used by the handler to run AfterClientClose while fields are intact.
	onClose func(*Connection)

	// client identification
	id         uint64    // unique client id (assigned atomically at creation)
	clientName string    // CLIENT SETNAME
	libName    string    // CLIENT SETINFO lib-name
	libVer     string    // CLIENT SETINFO lib-ver
	createdAt  time.Time // when the connection was established

	// replyMode controls CLIENT REPLY OFF/ON/SKIP (0=normal, 1=off, 2=skip)
	replyMode int

	// tracking state (CLIENT TRACKING)
	tracking     bool
	trackingMode int    // 0=default, 1=optin, 2=optout
	noLoop       bool   // NOLOOP flag
	redirectID   uint64 // redirect target client ID (0 = self)
}

var connPool = sync.Pool{
	New: func() interface{} {
		return &Connection{}
	},
}

// nextClientID is the atomic counter for assigning unique client IDs.
var nextClientID uint64

// clientRegistry tracks all live connections by their client ID.
var (
	clientRegistry   = make(map[uint64]*Connection)
	clientRegistryMu sync.RWMutex
)

// RegisterClient adds a connection to the process-wide client registry.
func RegisterClient(c *Connection) {
	clientRegistryMu.Lock()
	clientRegistry[c.id] = c
	clientRegistryMu.Unlock()
}

// UnregisterClient removes a connection from the registry by ID.
func UnregisterClient(id uint64) {
	clientRegistryMu.Lock()
	delete(clientRegistry, id)
	clientRegistryMu.Unlock()
}

// AllClients returns a snapshot of all currently registered connections.
func AllClients() []*Connection {
	clientRegistryMu.RLock()
	defer clientRegistryMu.RUnlock()
	clients := make([]*Connection, 0, len(clientRegistry))
	for _, c := range clientRegistry {
		clients = append(clients, c)
	}
	return clients
}

// ClientByID looks up a connection by its client ID, returning nil if not found.
func ClientByID(id uint64) *Connection {
	clientRegistryMu.RLock()
	defer clientRegistryMu.RUnlock()
	return clientRegistry[id]
}

// OnClose registers a callback that runs once inside Close()'s CAS guard,
// before fields are reset. Used by the handler to run AfterClientClose
// while the connection's fields are still intact.
func (c *Connection) OnClose(fn func(*Connection)) {
	c.onClose = fn
}

// RemoteAddr returns the remote network address
func (c *Connection) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().String()
}

// Close disconnect with the client. Safe to call concurrently — the cleanup
// and pool-return run exactly once.
func (c *Connection) Close() error {
	if !atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		return nil
	}
	UnregisterClient(c.id)
	if c.onClose != nil {
		c.onClose(c)
	}
	c.sendingData.WaitWithTimeout(10 * time.Second)
	if c.conn != nil { // may be a fake conn for tests
		_ = c.conn.Close()
	}
	c.subs = nil
	c.pSubs = nil
	c.password = ""
	c.queue = nil
	c.watching = nil
	c.txErrors = nil
	c.selectedDB = 0
	c.clientName = ""
	c.libName = ""
	c.libVer = ""
	c.replyMode = 0
	connPool.Put(c)
	return nil
}

// NewConn creates Connection instance
func NewConn(conn net.Conn) *Connection {
	c, ok := connPool.Get().(*Connection)
	if !ok {
		logger.Error("connection pool make wrong type")
		return &Connection{
			conn: conn,
		}
	}
	c.conn = conn
	c.closed = 0    // reset for reuse
	c.onClose = nil // clear stale callback from previous occupant
	c.flags = 0     // Close() never clears flags (inherited godis gap)
	c.protocol = redis.RESP2
	c.id = atomic.AddUint64(&nextClientID, 1)
	c.createdAt = time.Now()
	c.clientName = ""
	c.libName = ""
	c.libVer = ""
	c.replyMode = 0
	RegisterClient(c)
	return c
}

// Write sends response to client over tcp connection
func (c *Connection) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	c.sendingData.Add(1)
	defer func() {
		c.sendingData.Done()
	}()

	return c.conn.Write(b)
}

func (c *Connection) Name() string {
	if c.conn != nil {
		return c.conn.RemoteAddr().String()
	}
	return ""
}

// Subscribe add current connection into subscribers of the given channel
func (c *Connection) Subscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.subs == nil {
		c.subs = make(map[string]bool)
	}
	c.subs[channel] = true
}

// UnSubscribe removes current connection into subscribers of the given channel
func (c *Connection) UnSubscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.subs) == 0 {
		return
	}
	delete(c.subs, channel)
}

// SubsCount returns the number of subscribing channels
func (c *Connection) SubsCount() int {
	return len(c.subs)
}

// GetChannels returns all subscribing channels
func (c *Connection) GetChannels() []string {
	if c.subs == nil {
		return make([]string, 0)
	}
	channels := make([]string, len(c.subs))
	i := 0
	for channel := range c.subs {
		channels[i] = channel
		i++
	}
	return channels
}

// PSubscribe adds current connection into subscribers of the given pattern
func (c *Connection) PSubscribe(pattern string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pSubs == nil {
		c.pSubs = make(map[string]bool)
	}
	c.pSubs[pattern] = true
}

// PUnSubscribe removes current connection from subscribers of the given pattern
func (c *Connection) PUnSubscribe(pattern string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.pSubs) == 0 {
		return
	}
	delete(c.pSubs, pattern)
}

// PatternCount returns the number of pattern subscriptions
func (c *Connection) PatternCount() int {
	return len(c.pSubs)
}

// GetPatterns returns all subscribed patterns
func (c *Connection) GetPatterns() []string {
	if c.pSubs == nil {
		return make([]string, 0)
	}
	patterns := make([]string, len(c.pSubs))
	i := 0
	for pattern := range c.pSubs {
		patterns[i] = pattern
		i++
	}
	return patterns
}

// SetPassword stores password for authentication
func (c *Connection) SetPassword(password string) {
	c.password = password
}

// GetPassword get password for authentication
func (c *Connection) GetPassword() string {
	return c.password
}

// InMultiState tells is connection in an uncommitted transaction
func (c *Connection) InMultiState() bool {
	return c.flags&flagMulti > 0
}

// SetMultiState sets transaction flag
func (c *Connection) SetMultiState(state bool) {
	if !state { // reset data when cancel multi
		c.watching = nil
		c.queue = nil
		c.flags &= ^flagMulti // clean multi flag
		return
	}
	c.flags |= flagMulti
}

// GetQueuedCmdLine returns queued commands of current transaction
func (c *Connection) GetQueuedCmdLine() [][][]byte {
	return c.queue
}

// EnqueueCmd  enqueues command of current transaction
func (c *Connection) EnqueueCmd(cmdLine [][]byte) {
	c.queue = append(c.queue, cmdLine)
}

// AddTxError stores syntax error within transaction
func (c *Connection) AddTxError(err error) {
	c.txErrors = append(c.txErrors, err)
}

// GetTxErrors returns syntax error within transaction
func (c *Connection) GetTxErrors() []error {
	return c.txErrors
}

// ClearQueuedCmds clears queued commands of current transaction
func (c *Connection) ClearQueuedCmds() {
	c.queue = nil
}

// GetWatching returns watching keys and their version code when started watching
func (c *Connection) GetWatching() map[string]uint32 {
	if c.watching == nil {
		c.watching = make(map[string]uint32)
	}
	return c.watching
}

// GetDBIndex returns selected db
func (c *Connection) GetDBIndex() int {
	return c.selectedDB
}

// SelectDB selects a database
func (c *Connection) SelectDB(dbNum int) {
	c.selectedDB = dbNum
}

// Protocol returns the negotiated RESP version for this connection
func (c *Connection) Protocol() redis.RespVersion {
	if c.protocol == 0 {
		return redis.RESP2
	}
	return c.protocol
}

// SetProtocol sets the negotiated RESP version for this connection
func (c *Connection) SetProtocol(v redis.RespVersion) {
	c.protocol = v
}

func (c *Connection) SetSlave() {
	c.flags |= flagSlave
}

func (c *Connection) IsSlave() bool {
	return c.flags&flagSlave > 0
}

// SetMaster marks c as a connection with master
func (c *Connection) SetMaster() {
	c.flags |= flagMaster
}

func (c *Connection) IsMaster() bool {
	return c.flags&flagMaster > 0
}

// ClientID returns the unique client ID assigned at connection creation.
func (c *Connection) ClientID() uint64 { return c.id }

// ClientName returns the name set by CLIENT SETNAME.
func (c *Connection) ClientName() string { return c.clientName }

// SetClientName sets the client name (CLIENT SETNAME).
func (c *Connection) SetClientName(name string) { c.clientName = name }

// LibName returns the library name set by CLIENT SETINFO lib-name.
func (c *Connection) LibName() string { return c.libName }

// SetLibName sets the library name (CLIENT SETINFO lib-name).
func (c *Connection) SetLibName(name string) { c.libName = name }

// LibVer returns the library version set by CLIENT SETINFO lib-ver.
func (c *Connection) LibVer() string { return c.libVer }

// SetLibVer sets the library version (CLIENT SETINFO lib-ver).
func (c *Connection) SetLibVer(ver string) { c.libVer = ver }

// CreatedAt returns when the connection was established.
func (c *Connection) CreatedAt() time.Time { return c.createdAt }

// ReplyMode returns the CLIENT REPLY mode (0=normal, 1=off, 2=skip).
func (c *Connection) ReplyMode() int { return c.replyMode }

// SetReplyMode sets the CLIENT REPLY mode.
func (c *Connection) SetReplyMode(mode int) { c.replyMode = mode }

// IsTracking returns whether CLIENT TRACKING is enabled.
func (c *Connection) IsTracking() bool { return c.tracking }

// SetTracking enables or disables CLIENT TRACKING.
func (c *Connection) SetTracking(v bool) { c.tracking = v }

// TrackingMode returns the tracking mode (0=default, 1=optin, 2=optout).
func (c *Connection) TrackingMode() int { return c.trackingMode }

// SetTrackingMode sets the tracking mode.
func (c *Connection) SetTrackingMode(mode int) { c.trackingMode = mode }

// NoLoop returns the NOLOOP flag.
func (c *Connection) NoLoop() bool { return c.noLoop }

// SetNoLoop sets the NOLOOP flag.
func (c *Connection) SetNoLoop(v bool) { c.noLoop = v }

// RedirectID returns the redirect target client ID.
func (c *Connection) RedirectID() uint64 { return c.redirectID }

// SetRedirectID sets the redirect target client ID.
func (c *Connection) SetRedirectID(id uint64) { c.redirectID = id }
