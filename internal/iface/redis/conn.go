package redis

import "time"

// RespVersion indicates which RESP protocol version to use for encoding.
// Defined here (not in iface) to avoid an import cycle: iface/seams.go
// imports this package, so this package must not import iface.
type RespVersion uint8

const (
	RESP2 RespVersion = 2
	RESP3 RespVersion = 3
)

// Connection represents a connection with redis client
type Connection interface {
	Write([]byte) (int, error)
	Close() error
	RemoteAddr() string

	SetPassword(string)
	GetPassword() string

	// client should keep its subscribing channels
	Subscribe(channel string)
	UnSubscribe(channel string)
	SubsCount() int
	GetChannels() []string

	// pattern subscriptions
	PSubscribe(pattern string)
	PUnSubscribe(pattern string)
	PatternCount() int
	GetPatterns() []string

	InMultiState() bool
	SetMultiState(bool)
	GetQueuedCmdLine() [][][]byte
	EnqueueCmd([][]byte)
	ClearQueuedCmds()
	GetWatching() map[string]uint32
	AddTxError(err error)
	GetTxErrors() []error

	GetDBIndex() int
	SelectDB(int)

	Protocol() RespVersion
	SetProtocol(RespVersion)

	SetSlave()
	IsSlave() bool

	SetMaster()
	IsMaster() bool

	Name() string

	// client identification
	ClientID() uint64
	ClientName() string
	SetClientName(string)
	LibName() string
	SetLibName(string)
	LibVer() string
	SetLibVer(string)
	CreatedAt() time.Time

	// reply mode for CLIENT REPLY OFF/ON/SKIP
	ReplyMode() int
	SetReplyMode(int)
}
