package diff

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
)

type Command struct {
	Args      []string
	Normalize func([]byte) []byte // optional; applied before comparison
}

// sortRespArray parses a RESP array and returns a new RESP array with the
// top-level elements sorted lexicographically. Only works for flat arrays
// of bulk strings (e.g. KEYS output).
func sortRespArray(raw []byte) []byte {
	if len(raw) == 0 || raw[0] != '*' {
		return raw
	}
	idx := bytes.IndexByte(raw, '\r')
	if idx < 0 {
		return raw
	}
	n, err := strconv.Atoi(string(raw[1:idx]))
	if err != nil || n <= 0 {
		return raw
	}
	// Extract each bulk string element
	elements := make([]string, 0, n)
	pos := idx + 2 // skip \r\n
	for i := 0; i < n && pos < len(raw); i++ {
		if pos >= len(raw) || raw[pos] != '$' {
			return raw
		}
		end := bytes.IndexByte(raw[pos:], '\r')
		if end < 0 {
			return raw
		}
		sLen, err := strconv.Atoi(string(raw[pos+1 : pos+end]))
		if err != nil {
			return raw
		}
		dataStart := pos + end + 2
		if sLen < 0 {
			elements = append(elements, "")
			pos = dataStart
		} else {
			elements = append(elements, string(raw[dataStart:dataStart+sLen]))
			pos = dataStart + sLen + 2
		}
	}
	sort.Strings(elements)
	var b strings.Builder
	b.WriteString("*")
	b.WriteString(strconv.Itoa(len(elements)))
	b.WriteString("\r\n")
	for _, e := range elements {
		b.WriteString("$")
		b.WriteString(strconv.Itoa(len(e)))
		b.WriteString("\r\n")
		b.WriteString(e)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// normalizeScan normalizes a SCAN reply: replaces the cursor with "0" and
// sorts the returned keys. SCAN output is non-deterministic in both cursor
// and key order.
func normalizeScan(raw []byte) []byte {
	if len(raw) == 0 || raw[0] != '*' {
		return raw
	}
	// Parse top-level array count
	idx := bytes.IndexByte(raw, '\r')
	if idx < 0 {
		return raw
	}
	n, err := strconv.Atoi(string(raw[1:idx]))
	if err != nil || n != 2 {
		return raw
	}
	pos := idx + 2
	// First element: cursor bulk string — skip it
	if pos >= len(raw) || raw[pos] != '$' {
		return raw
	}
	end := bytes.IndexByte(raw[pos:], '\r')
	if end < 0 {
		return raw
	}
	cLen, _ := strconv.Atoi(string(raw[pos+1 : pos+end]))
	cursorEnd := pos + end + 2 + cLen + 2
	// Second element: array of keys — sort it
	keysRaw := sortRespArray(raw[cursorEnd:])
	return []byte("*2\r\n$1\r\n0\r\n" + string(keysRaw))
}

type Scenario struct {
	Name     string
	Commands []Command
}

func baseCorpus() []Scenario {
	return []Scenario{
		{Name: "ping", Commands: []Command{{Args: []string{"PING"}}}},
		{Name: "string set get del", Commands: []Command{
			{Args: []string{"SET", "s", "v"}},
			{Args: []string{"GET", "s"}},
			{Args: []string{"DEL", "s"}},
			{Args: []string{"GET", "s"}},
		}},
		{Name: "string numeric", Commands: []Command{
			{Args: []string{"SET", "n", "1"}},
			{Args: []string{"INCR", "n"}},
			{Args: []string{"DECRBY", "n", "2"}},
			{Args: []string{"GET", "n"}},
		}},
		{Name: "hash", Commands: []Command{
			{Args: []string{"HSET", "h", "f", "v"}},
			{Args: []string{"HGET", "h", "f"}},
			{Args: []string{"HLEN", "h"}},
			{Args: []string{"HDEL", "h", "f"}},
		}},
		{Name: "list", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "b", "c"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
			{Args: []string{"LPOP", "l"}},
			{Args: []string{"LLEN", "l"}},
		}},
		{Name: "set sorted response", Commands: []Command{
			{Args: []string{"SADD", "set", "a", "b"}},
			{Args: []string{"SISMEMBER", "set", "a"}},
			{Args: []string{"SCARD", "set"}},
			{Args: []string{"SREM", "set", "a"}},
		}},
		{Name: "zset", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b"}},
			{Args: []string{"ZRANGE", "z", "0", "-1"}},
			{Args: []string{"ZSCORE", "z", "a"}},
			{Args: []string{"ZREM", "z", "a"}},
		}},
		{Name: "keys ttl", Commands: []Command{
			{Args: []string{"SET", "ttl", "v"}},
			{Args: []string{"EXPIRE", "ttl", "600"}},
			{Args: []string{"PERSIST", "ttl"}},
			{Args: []string{"TTL", "ttl"}},
		}},
		{Name: "wrong type", Commands: []Command{
			{Args: []string{"SET", "wrong", "v"}},
			{Args: []string{"LPUSH", "wrong", "x"}},
		}},
		{Name: "arity error", Commands: []Command{
			{Args: []string{"GET"}},
			{Args: []string{"SET", "only-key"}},
		}},
	}
}
