package server

import (
	"sync"

	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/iface/redis"
)

// lockedEngine wraps a StorageEngine with a single mutex,
// serialising all Exec/AfterClientClose/Close calls.
// Used for the redisdb engine which uses a single non-concurrent dict.
type lockedEngine struct {
	mu    sync.Mutex
	inner iface.StorageEngine
}

// NewLockedEngine wraps an engine with a global lock.
func NewLockedEngine(inner iface.StorageEngine) iface.StorageEngine {
	return &lockedEngine{inner: inner}
}

// Inner returns the wrapped engine so callers can unwrap to the concrete type
// (e.g. *database.Server) when needed for keyspace introspection wiring.
func (le *lockedEngine) Inner() iface.StorageEngine {
	return le.inner
}

func (le *lockedEngine) Exec(client redis.Connection, cmdLine iface.CmdLine) redis.Reply {
	le.mu.Lock()
	defer le.mu.Unlock()
	return le.inner.Exec(client, cmdLine)
}

func (le *lockedEngine) AfterClientClose(client redis.Connection) {
	le.mu.Lock()
	defer le.mu.Unlock()
	le.inner.AfterClientClose(client)
}

func (le *lockedEngine) Close() {
	le.mu.Lock()
	defer le.mu.Unlock()
	le.inner.Close()
}
