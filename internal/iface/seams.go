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
// DESIGN DECISION: This seam is intentionally cut at the "execute a command"
// altitude (godis DB.Exec), not at the lower Get/Set/Del/Expire level. The
// godis baseline bundles command dispatch and storage in one package
// (internal/command), so the baseline implementation =
// database.NewStandaloneServer() which is the full DB including all handlers.
//
// Consequence: the redisdb backend does not reimplement command dispatch; it
// switches the underlying dict via the factory in internal/datastruct/dict
// (SetEngine/MakeDict) while sharing all of internal/command.
//
// This is a deliberate strangler-pattern shortcut: keep godis code intact and
// add seam adapters around it instead of re-cutting the seam lower.
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

// ScriptInvoker runs one command through the normal server execution path on
// behalf of a script's redis.call/pcall. Its writes propagate to AOF/replicas
// (effects replication) because the path goes through db.addAof.
type ScriptInvoker func(client iredis.Connection, cmdLine CmdLine) iredis.Reply

// ScriptEngine is the seam for server-side scripting (default: gopher-lua; a
// cgo/liblua impl can replace it without touching the command layer).
type ScriptEngine interface {
	Eval(client iredis.Connection, body string, keys, args []string) iredis.Reply
	EvalSha(client iredis.Connection, sha string, keys, args []string) iredis.Reply
	Load(body string) (sha string)
	Exists(shas []string) []bool
	Flush()
	Kill() error
}

// Compile-time check: database.DB satisfies StorageEngine.
var _ StorageEngine = (idatabase.DB)(nil)
