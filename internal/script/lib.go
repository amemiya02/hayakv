package script

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	lua "github.com/yuin/gopher-lua"
)

// setKeysArgv populates the global KEYS and ARGV tables.
func setKeysArgv(L *lua.LState, keys, args []string) {
	kt := L.NewTable()
	for _, k := range keys {
		kt.Append(lua.LString(k))
	}
	at := L.NewTable()
	for _, a := range args {
		at.Append(lua.LString(a))
	}
	L.SetGlobal("KEYS", kt)
	L.SetGlobal("ARGV", at)
}

// registerRedis registers the "redis" (and "server" alias) module.
func (e *Engine) registerRedis(L *lua.LState, c iredis.Connection, readonly bool) {
	mod := L.NewTable()

	// Command execution
	mod.RawSetString("call", L.NewFunction(e.luaCall(c, false, readonly)))
	mod.RawSetString("pcall", L.NewFunction(e.luaCall(c, true, readonly)))

	// Reply helpers
	mod.RawSetString("error_reply", L.NewFunction(luaErrorReply))
	mod.RawSetString("status_reply", L.NewFunction(luaStatusReply))

	// Utilities
	mod.RawSetString("sha1hex", L.NewFunction(luaSha1Hex))
	mod.RawSetString("log", L.NewFunction(luaLog))
	mod.RawSetString("setresp", L.NewFunction(luaSetRespStub))
	mod.RawSetString("breakpoint", L.NewFunction(luaNoop))
	mod.RawSetString("debug", L.NewFunction(luaNoop))

	// Log level constants
	mod.RawSetString("LOG_DEBUG", lua.LNumber(0))
	mod.RawSetString("LOG_VERBOSE", lua.LNumber(1))
	mod.RawSetString("LOG_NOTICE", lua.LNumber(2))
	mod.RawSetString("LOG_WARNING", lua.LNumber(3))

	L.SetGlobal("redis", mod)
	L.SetGlobal("server", mod) // Redis 7.4 alias

	// Register cjson as a sub-module
	registerCJSON(L)
}

// luaCall returns a LGFunction for redis.call or redis.pcall.
func (e *Engine) luaCall(c iredis.Connection, protected, readonly bool) lua.LGFunction {
	return func(L *lua.LState) int {
		n := L.GetTop()
		if n == 0 {
			if protected {
				L.Push(lua.LFalse)
				L.Push(lua.LString("redis.pcall requires at least one argument"))
				return 2
			}
			L.RaiseError("redis.call requires at least one argument")
			return 0
		}
		cmdLine := make([][]byte, 0, n)
		for i := 1; i <= n; i++ {
			cmdLine = append(cmdLine, []byte(L.CheckString(i)))
		}
		if readonly && !isReadOnlyLuaCmd(cmdLine) {
			errMsg := "ERR Write commands are not allowed from read-only scripts."
			if protected {
				L.Push(lua.LFalse)
				L.Push(lua.LString(errMsg))
				return 2
			}
			L.RaiseError("%s", errMsg)
			return 0
		}
		reply := e.invoker(c, cmdLine)
		if isErrReply(reply) {
			if protected {
				L.Push(respToLua(L, reply))
				return 1
			}
			L.RaiseError("%s", errText(reply))
			return 0
		}
		L.Push(respToLua(L, reply))
		return 1
	}
}

// isErrReply checks if a reply starts with '-' (RESP error).
func isErrReply(r iredis.Reply) bool {
	b := r.ToBytes()
	return len(b) > 0 && b[0] == '-'
}

// errText extracts the error message from a RESP error reply.
func errText(r iredis.Reply) string {
	b := r.ToBytes()
	if len(b) > 2 {
		return strings.TrimSuffix(string(b[1:]), "\r\n")
	}
	return ""
}

// isReadOnlyLuaCmd reports whether a command issued from a Lua script is
// read-only (i.e. allowed inside EVAL_RO / EVALSHA_RO).
// Uses a write-command set: if the command IS in this set, it's NOT read-only.
func isReadOnlyLuaCmd(cmdLine [][]byte) bool {
	if len(cmdLine) == 0 {
		return false
	}
	name := strings.ToLower(string(cmdLine[0]))
	writeCmds := map[string]bool{
		// strings
		"set": true, "setex": true, "psetex": true, "setnx": true, "mset": true,
		"msetnx": true, "append": true, "incr": true, "incrby": true, "incrbyfloat": true,
		"decr": true, "decrby": true, "getdel": true, "getset": true, "setrange": true,
		"del": true, "unlink": true, "expire": true, "expireat": true, "pexpire": true,
		"pexpireat": true, "persist": true, "rename": true, "renamenx": true,
		// hashes
		"hset": true, "hmset": true, "hsetnx": true, "hincrby": true, "hincrbyfloat": true,
		"hdel": true,
		// lists
		"lpush": true, "lpushx": true, "rpush": true, "rpushx": true, "lpop": true,
		"rpop": true, "lset": true, "linsert": true, "lrem": true, "ltrim": true,
		"rpoplpush": true, "lmove": true, "blmove": true, "blpop": true, "brpop": true,
		"brpoplpush": true, "lmpop": true, "blmpop": true,
		// sets
		"sadd": true, "srem": true, "smove": true, "spop": true,
		"sinterstore": true, "sunionstore": true, "sdiffstore": true,
		// sorted sets
		"zadd": true, "zrem": true, "zincrby": true, "zremrangebyrank": true,
		"zremrangebyscore": true, "zremrangebylex": true, "zunionstore": true,
		"zinterstore": true, "zdiffstore": true, "zrangestore": true,
		"zmpop": true, "bzmpop": true, "bzpopmin": true, "bzpopmax": true,
		// hyperloglog
		"pfadd": true,
		// geo
		"geoadd": true,
		// streams
		"xadd": true, "xdel": true, "xtrim": true, "xgroup": true, "xsetid": true,
		"xack": true,
		// scripting
		"eval": true, "evalsha": true, "script": true,
		// pub/sub
		"subscribe": true, "unsubscribe": true, "psubscribe": true, "punsubscribe": true,
		"publish": true,
		// server
		"flushdb": true, "flushall": true, "swapdb": true, "debug": true,
		"bgrewriteaof": true, "bgsave": true, "save": true, "shutdown": true,
		"slaveof": true, "replicaof": true, "migrate": true, "restore": true,
		"sort": true, // SORT with STORE is a write
	}
	// If it's a known write command, it's NOT read-only.
	return !writeCmds[name]
}

// luaErrorReply implements redis.error_reply(msg).
func luaErrorReply(L *lua.LState) int {
	msg := L.CheckString(1)
	tb := L.NewTable()
	tb.RawSetString("err", lua.LString(msg))
	L.Push(tb)
	return 1
}

// luaStatusReply implements redis.status_reply(msg).
func luaStatusReply(L *lua.LState) int {
	msg := L.CheckString(1)
	tb := L.NewTable()
	tb.RawSetString("ok", lua.LString(msg))
	L.Push(tb)
	return 1
}

// luaSha1Hex implements redis.sha1hex(msg).
func luaSha1Hex(L *lua.LState) int {
	msg := L.CheckString(1)
	s := sha1.Sum([]byte(msg))
	L.Push(lua.LString(hex.EncodeToString(s[:])))
	return 1
}

// luaLog implements redis.log(level, msg). Currently a no-op.
func luaLog(L *lua.LState) int {
	return 0
}

// luaSetRespStub implements redis.setresp(). Stub for RESP version switching.
func luaSetRespStub(L *lua.LState) int {
	return 0
}

// luaNoop is a no-op function for redis.breakpoint and redis.debug.
func luaNoop(L *lua.LState) int {
	return 0
}

// --- cjson module ---

// registerCJSON registers a minimal cjson module globally.
func registerCJSON(L *lua.LState) {
	mod := L.NewTable()
	mod.RawSetString("encode", L.NewFunction(cjsonEncode))
	mod.RawSetString("decode", L.NewFunction(cjsonDecode))
	mod.RawSetString("null", lua.LFalse) // cjson.null sentinel
	L.SetGlobal("cjson", mod)
}

// cjsonEncode converts a Lua value to a JSON string.
func cjsonEncode(L *lua.LState) int {
	v := L.CheckAny(1)
	j := luaToJSON(v)
	data, err := json.Marshal(j)
	if err != nil {
		L.RaiseError("cjson.encode error: %v", err)
		return 0
	}
	L.Push(lua.LString(data))
	return 1
}

// cjsonDecode parses a JSON string into a Lua value.
func cjsonDecode(L *lua.LState) int {
	s := L.CheckString(1)
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		L.RaiseError("cjson.decode error: %v", err)
		return 0
	}
	L.Push(jsonToLua(L, v))
	return 1
}

// luaToJSON converts a Lua value to a Go value suitable for json.Marshal.
func luaToJSON(v lua.LValue) interface{} {
	switch val := v.(type) {
	case lua.LBool:
		return bool(val)
	case lua.LNumber:
		return float64(val)
	case lua.LString:
		return string(val)
	case *lua.LTable:
		// Check if it looks like an array (sequential integer keys)
		maxN := val.MaxN()
		if maxN > 0 {
			arr := make([]interface{}, 0, maxN)
			for i := 1; i <= maxN; i++ {
				arr = append(arr, luaToJSON(val.RawGetInt(i)))
			}
			return arr
		}
		// Otherwise treat as object
		m := make(map[string]interface{})
		val.ForEach(func(key, value lua.LValue) {
			if ks, ok := key.(lua.LString); ok {
				m[string(ks)] = luaToJSON(value)
			}
		})
		return m
	default:
		return nil
	}
}

// jsonToLua converts a Go value (from json.Unmarshal) to a Lua value.
func jsonToLua(L *lua.LState, v interface{}) lua.LValue {
	switch val := v.(type) {
	case nil:
		return lua.LFalse // cjson.null
	case bool:
		if val {
			return lua.LTrue
		}
		return lua.LFalse
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []interface{}:
		tb := L.NewTable()
		for _, item := range val {
			tb.Append(jsonToLua(L, item))
		}
		return tb
	case map[string]interface{}:
		tb := L.NewTable()
		for k, item := range val {
			tb.RawSetString(k, jsonToLua(L, item))
		}
		return tb
	default:
		// Fallback: stringify
		return lua.LString(fmt.Sprintf("%v", val))
	}
}
