package script

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"strconv"
	"strings"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	lua "github.com/yuin/gopher-lua"
)

// luaToRESP converts a Lua value to a RESP reply.
// Rules (Redis 7, RESP2): number->:int (truncated); string->bulk;
// table->array (stops at first nil); true->:1; false/nil->$-1;
// {err=m}->-m; {ok=s}->+s.
func luaToRESP(L *lua.LState, v lua.LValue) iredis.Reply {
	switch val := v.(type) {
	case lua.LBool:
		if bool(val) {
			return protocol.MakeIntReply(1)
		}
		return protocol.MakeNullBulkReply()
	case lua.LNumber:
		return protocol.MakeIntReply(int64(math.Floor(float64(val))))
	case lua.LString:
		return protocol.MakeBulkReply([]byte(string(val)))
	case *lua.LTable:
		// Check for {err=msg} pattern
		if f := val.RawGetString("err"); f != lua.LNil {
			if s, ok := f.(lua.LString); ok {
				return protocol.MakeErrReply(string(s))
			}
		}
		// Check for {ok=status} pattern
		if f := val.RawGetString("ok"); f != lua.LNil {
			if s, ok := f.(lua.LString); ok {
				return protocol.MakeStatusReply(string(s))
			}
		}
		// Array: sequential integer keys starting at 1
		var elems []iredis.Reply
		for i := 1; ; i++ {
			item := val.RawGetInt(i)
			if item == lua.LNil {
				break
			}
			elems = append(elems, luaToRESP(L, item))
		}
		if len(elems) == 0 {
			return protocol.MakeEmptyMultiBulkReply()
		}
		return protocol.MakeMultiRawReply(elems)
	default:
		return protocol.MakeNullBulkReply()
	}
}

// respToLua converts a RESP reply back into a Lua value by parsing its wire bytes.
func respToLua(L *lua.LState, reply iredis.Reply) lua.LValue {
	return parseRESPToLua(L, reply.ToBytes())
}

// parseRESPToLua walks RESP wire bytes and returns a Lua value.
func parseRESPToLua(L *lua.LState, raw []byte) lua.LValue {
	r := bufio.NewReader(bytes.NewReader(raw))
	v, err := readOneRESP(L, r)
	if err != nil {
		return lua.LFalse
	}
	return v
}

// readOneRESP reads a single RESP value from the reader.
func readOneRESP(L *lua.LState, r *bufio.Reader) (lua.LValue, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	// Trim CRLF
	line = bytes.TrimRight(line, "\r\n")

	switch prefix {
	case '+': // Status reply -> {ok=status}
		tb := L.NewTable()
		tb.RawSetString("ok", lua.LString(line))
		return tb, nil

	case '-': // Error reply -> {err=msg}
		tb := L.NewTable()
		tb.RawSetString("err", lua.LString(line))
		return tb, nil

	case ':': // Integer reply -> number
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return lua.LNumber(0), nil
		}
		return lua.LNumber(n), nil

	case '$': // Bulk string
		n, err := strconv.Atoi(string(line))
		if err != nil {
			return nil, err
		}
		if n < 0 {
			// Null bulk string -> false
			return lua.LFalse, nil
		}
		payload := make([]byte, n+2) // +2 for trailing CRLF
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		// Strip trailing CRLF
		return lua.LString(payload[:n]), nil

	case '*': // Array
		n, err := strconv.Atoi(string(line))
		if err != nil {
			return nil, err
		}
		if n < 0 {
			// Null array -> false
			return lua.LFalse, nil
		}
		tb := L.NewTable()
		for i := 0; i < n; i++ {
			child, err := readOneRESP(L, r)
			if err != nil {
				return nil, err
			}
			tb.Append(child)
		}
		return tb, nil

	default:
		// Unsupported prefix; return false
		return lua.LFalse, nil
	}
}

// formatLuaError wraps a Lua error into a Redis-style ERR message.
func formatLuaError(err error) string {
	msg := err.Error()
	// Strip the gopher-lua file:line prefix if present
	if idx := strings.Index(msg, ":"); idx > 0 {
		rest := msg[idx+1:]
		if idx2 := strings.Index(rest, ":"); idx2 > 0 {
			msg = strings.TrimSpace(rest[idx2+1:])
		}
	}
	return "ERR Error running script: " + msg
}
