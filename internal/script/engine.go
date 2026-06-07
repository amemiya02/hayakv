package script

import (
	"crypto/sha1"
	"encoding/hex"
	"sync"

	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	lua "github.com/yuin/gopher-lua"
)

// Engine implements iface.ScriptEngine using gopher-lua.
type Engine struct {
	invoker iface.ScriptInvoker
	mu      sync.Mutex
	cache   map[string]string // sha1 -> script body
	busyMs  int64
	killReq bool
}

// NewEngine creates a new gopher-lua backed ScriptEngine.
func NewEngine(invoker iface.ScriptInvoker, busyMs int64) *Engine {
	return &Engine{
		invoker: invoker,
		cache:   map[string]string{},
		busyMs:  busyMs,
	}
}

func sha1hex(body string) string {
	s := sha1.Sum([]byte(body))
	return hex.EncodeToString(s[:])
}

// Load stores a script body and returns its SHA1 hex digest.
func (e *Engine) Load(body string) string {
	sha := sha1hex(body)
	e.mu.Lock()
	e.cache[sha] = body
	e.mu.Unlock()
	return sha
}

// Exists checks whether each SHA1 is present in the cache.
func (e *Engine) Exists(shas []string) []bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]bool, len(shas))
	for i, s := range shas {
		_, out[i] = e.cache[s]
	}
	return out
}

// Flush clears the script cache.
func (e *Engine) Flush() {
	e.mu.Lock()
	e.cache = map[string]string{}
	e.mu.Unlock()
}

// Kill requests that any running script be terminated.
func (e *Engine) Kill() error {
	e.mu.Lock()
	e.killReq = true
	e.mu.Unlock()
	return nil
}

// Eval loads (if not already cached) and runs a script.
// When readonly is true, redis.call/pcall rejects write commands.
func (e *Engine) Eval(c iredis.Connection, body string, keys, args []string, readonly bool) iredis.Reply {
	e.Load(body)
	return e.run(c, body, keys, args, readonly)
}

// EvalSha runs a previously loaded script by SHA1.
// When readonly is true, redis.call/pcall rejects write commands.
func (e *Engine) EvalSha(c iredis.Connection, sha string, keys, args []string, readonly bool) iredis.Reply {
	e.mu.Lock()
	body, ok := e.cache[sha]
	e.mu.Unlock()
	if !ok {
		return protocol.MakeErrReply("NOSCRIPT No matching script. Please use EVAL.")
	}
	return e.run(c, body, keys, args, readonly)
}

// run creates a fresh Lua state, registers the redis library, and executes.
func (e *Engine) run(c iredis.Connection, body string, keys, args []string, readonly bool) iredis.Reply {
	L := lua.NewState(lua.Options{SkipOpenLibs: false})
	defer L.Close()

	e.registerRedis(L, c, readonly)
	setKeysArgv(L, keys, args)

	if err := L.DoString(body); err != nil {
		return protocol.MakeErrReply(formatLuaError(err))
	}

	// Return the top-of-stack value converted to RESP
	top := L.Get(-1)
	if top == lua.LNil {
		return protocol.MakeNullBulkReply()
	}
	return luaToRESP(L, top)
}
