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

// RespVersion indicates which RESP protocol version to use for encoding.
// Aliased from iface/redis to avoid import cycle.
type RespVersion = iredis.RespVersion

const (
	RESP2 = iredis.RESP2
	RESP3 = iredis.RESP3
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
//
// M0 DESIGN DECISION: This seam is intentionally cut at the "execute a command"
// altitude (godis DB.Exec), not at the lower Get/Set/Del/Expire level described
// in spec §3.2/§3.3. The godis baseline bundles command dispatch and storage in
// one package (internal/command), so the M0 implementation =
// database.NewStandaloneServer() which is the full DB including all handlers.
//
// This means M2's redisdb backend will need to either:
//   - reimplement command dispatch, or
//   - re-cut the seam lower (extract an internal/engine/{shardmap,redisdb} layer)
//     so handlers are shared across engines.
//
// This is a deliberate M0 strangler-pattern shortcut: keep godis code intact,
// add seam adapters around it, and defer the deeper refactoring to M2 when the
// redisdb backend actually needs it.
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
