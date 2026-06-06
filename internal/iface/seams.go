package iface

import (
	"context"
	"io"
	"net"

	idatabase "github.com/amemiya02/hayakv/internal/iface/database"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
)

// CmdLine is alias for [][]byte, represents a command line
type CmdLine = [][]byte

// RespVersion indicates which RESP protocol version to use for encoding
type RespVersion uint8

const (
	RESP2 RespVersion = 2
	RESP3 RespVersion = 3
)

// NetServer is the seam for the TCP listener layer.
type NetServer interface {
	Run(ctx context.Context, addr string, handler NetHandler) error
	Close() error
}

// NetHandler is the seam for per-connection request handling.
type NetHandler interface {
	Handle(ctx context.Context, conn net.Conn)
	Close() error
}

// ProtocolPayload carries a decoded reply or a decoding error from the codec stream.
type ProtocolPayload struct {
	Reply iredis.Reply
	Err   error
}

// ProtocolCodec is the seam between wire bytes and domain replies.
type ProtocolCodec interface {
	DecodeStream(reader io.Reader) <-chan ProtocolPayload
	DecodeOne(data []byte) (iredis.Reply, error)
	Encode(reply iredis.Reply, resp RespVersion) []byte
}

// StorageEngine is the seam for the command execution back-end.
type StorageEngine interface {
	Exec(client iredis.Connection, cmdLine CmdLine) iredis.Reply
	AfterClientClose(client iredis.Connection)
	Close()
}

// Object is the seam for inspecting a stored value's type and encoding.
type Object interface {
	TypeName() string
	EncodingName() string
	Value() any
}

// Compile-time check: database.DB satisfies StorageEngine.
var _ StorageEngine = (idatabase.DB)(nil)
