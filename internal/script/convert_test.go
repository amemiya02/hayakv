package script

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestLuaToRESP(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	cases := []struct {
		name string
		push func() lua.LValue
		want string
	}{
		{"number truncated", func() lua.LValue { return lua.LNumber(3.9) }, ":3\r\n"},
		{"string", func() lua.LValue { return lua.LString("hi") }, "$2\r\nhi\r\n"},
		{"true", func() lua.LValue { return lua.LTrue }, ":1\r\n"},
		{"false", func() lua.LValue { return lua.LFalse }, "$-1\r\n"},
		{"nil", func() lua.LValue { return lua.LNil }, "$-1\r\n"},
		{"array", func() lua.LValue {
			tb := L.NewTable()
			tb.Append(lua.LNumber(1))
			tb.Append(lua.LString("a"))
			return tb
		}, "*2\r\n:1\r\n$1\r\na\r\n"},
		{"err table", func() lua.LValue {
			tb := L.NewTable()
			tb.RawSetString("err", lua.LString("My Error"))
			return tb
		}, "-My Error\r\n"},
		{"ok table", func() lua.LValue {
			tb := L.NewTable()
			tb.RawSetString("ok", lua.LString("fine"))
			return tb
		}, "+fine\r\n"},
		{"empty table", func() lua.LValue {
			return L.NewTable()
		}, "*0\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(luaToRESP(L, c.push()).ToBytes())
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestParseRESPToLua(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("status", func(t *testing.T) {
		v := parseRESPToLua(L, []byte("+OK\r\n"))
		tb, ok := v.(*lua.LTable)
		if !ok {
			t.Fatalf("expected table, got %T", v)
		}
		if s := tb.RawGetString("ok"); string(s.(lua.LString)) != "OK" {
			t.Errorf("ok = %q", s)
		}
	})

	t.Run("error", func(t *testing.T) {
		v := parseRESPToLua(L, []byte("-ERR something\r\n"))
		tb, ok := v.(*lua.LTable)
		if !ok {
			t.Fatalf("expected table, got %T", v)
		}
		if s := tb.RawGetString("err"); string(s.(lua.LString)) != "ERR something" {
			t.Errorf("err = %q", s)
		}
	})

	t.Run("integer", func(t *testing.T) {
		v := parseRESPToLua(L, []byte(":42\r\n"))
		if n, ok := v.(lua.LNumber); !ok || int(n) != 42 {
			t.Errorf("got %v", v)
		}
	})

	t.Run("bulk string", func(t *testing.T) {
		v := parseRESPToLua(L, []byte("$3\r\nfoo\r\n"))
		if s := string(v.(lua.LString)); s != "foo" {
			t.Errorf("got %q", s)
		}
	})

	t.Run("null bulk", func(t *testing.T) {
		v := parseRESPToLua(L, []byte("$-1\r\n"))
		if v != lua.LFalse {
			t.Errorf("expected false, got %v", v)
		}
	})

	t.Run("array", func(t *testing.T) {
		v := parseRESPToLua(L, []byte("*2\r\n$1\r\na\r\n:9\r\n"))
		tb, ok := v.(*lua.LTable)
		if !ok {
			t.Fatalf("expected table, got %T", v)
		}
		if s := string(tb.RawGetInt(1).(lua.LString)); s != "a" {
			t.Errorf("elem 1 = %q", s)
		}
		if n := tb.RawGetInt(2).(lua.LNumber); int(n) != 9 {
			t.Errorf("elem 2 = %v", n)
		}
	})

	t.Run("null array", func(t *testing.T) {
		v := parseRESPToLua(L, []byte("*-1\r\n"))
		if v != lua.LFalse {
			t.Errorf("expected false, got %v", v)
		}
	})
}

func TestRespToLuaRoundTrip(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Test that a multi-bulk reply round-trips through Lua correctly
	raw := []byte("*3\r\n$3\r\nfoo\r\n:42\r\n$-1\r\n")
	v := parseRESPToLua(L, raw)
	tb, ok := v.(*lua.LTable)
	if !ok {
		t.Fatalf("expected table, got %T", v)
	}
	if s := string(tb.RawGetInt(1).(lua.LString)); s != "foo" {
		t.Errorf("elem 1 = %q", s)
	}
	if n := tb.RawGetInt(2).(lua.LNumber); int(n) != 42 {
		t.Errorf("elem 2 = %v", n)
	}
	if tb.RawGetInt(3) != lua.LFalse {
		t.Errorf("elem 3 should be false for null bulk")
	}
}
