package diff

import (
	"bytes"
	"strconv"
	"testing"
)

// normalizeTTLArray clamps positive TTL values in a RESP array to "1" while
// preserving negative status codes (-1 = no expiry, -2 = missing field/key).
// This absorbs the few-ms timing drift between hayakv and real Redis 8.
//
// Input is a RESP array of integers, e.g.:
//
//	*2\r\n:100\r\n:-1\r\n
//
// Output replaces any positive integer with :1\r\n.
func normalizeTTLArray(raw []byte) []byte {
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
	pos := idx + 2 // skip \r\n after *N
	var out bytes.Buffer
	out.WriteString("*")
	out.WriteString(strconv.Itoa(n))
	out.WriteString("\r\n")
	for i := 0; i < n && pos < len(raw); i++ {
		if pos >= len(raw) || raw[pos] != ':' {
			return raw
		}
		end := bytes.IndexByte(raw[pos:], '\r')
		if end < 0 {
			return raw
		}
		val := string(raw[pos+1 : pos+end])
		v, err := strconv.Atoi(val)
		if err != nil {
			return raw
		}
		if v > 0 {
			out.WriteString(":1\r\n")
		} else {
			out.Write(raw[pos : pos+end+2])
		}
		pos = pos + end + 2
	}
	return out.Bytes()
}

func hashTTLCorpus() []Scenario {
	return []Scenario{
		// --- HExpire / HTTL / HPersist family ---
		{Name: "hexpire basic", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1", "f2", "v2"}},
			{Args: []string{"HEXPIRE", "h", "100", "FIELDS", "1", "f1"}},
			{Args: []string{"HTTL", "h", "FIELDS", "2", "f1", "f2"}, Normalize: normalizeTTLArray},
			{Args: []string{"HPERSIST", "h", "FIELDS", "1", "f1"}},
			{Args: []string{"HTTL", "h", "FIELDS", "1", "f1"}, Normalize: normalizeTTLArray},
		}},
		{Name: "hexpire missing key", Commands: []Command{
			{Args: []string{"HEXPIRE", "nope", "100", "FIELDS", "1", "f1"}},
			{Args: []string{"HTTL", "nope", "FIELDS", "1", "f1"}},
		}},
		// --- HPExpire / HPTTL ---
		{Name: "hpexpire basic", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1", "f2", "v2"}},
			{Args: []string{"HPEXPIRE", "h", "60000", "FIELDS", "1", "f1"}},
			{Args: []string{"HPTTL", "h", "FIELDS", "2", "f1", "f2"}, Normalize: normalizeTTLArray},
		}},
		// --- HExpireAt / HExpireTime ---
		{Name: "hexpireat hexpiretime", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1"}},
			{Args: []string{"HEXPIREAT", "h", "4070908800", "FIELDS", "1", "f1"}},
			{Args: []string{"HEXPIRETIME", "h", "FIELDS", "1", "f1"}},
		}},
		// --- HPExpireAt / HPExpireTime ---
		{Name: "hpexpireat hpexpiretime", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1"}},
			{Args: []string{"HPEXPIREAT", "h", "4070908800000", "FIELDS", "1", "f1"}},
			{Args: []string{"HPEXPIRETIME", "h", "FIELDS", "1", "f1"}},
		}},
		// --- HExpire NX/XX/GT/LT options ---
		{Name: "hexpire options", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1"}},
			{Args: []string{"HEXPIRE", "h", "100", "FIELDS", "1", "f1"}},
			{Args: []string{"HEXPIRE", "h", "200", "NX", "FIELDS", "1", "f1"}},
			{Args: []string{"HEXPIRE", "h", "200", "XX", "FIELDS", "1", "f1"}},
			{Args: []string{"HEXPIRE", "h", "300", "GT", "FIELDS", "1", "f1"}},
			{Args: []string{"HEXPIRE", "h", "100", "GT", "FIELDS", "1", "f1"}},
		}},
		// --- HGetDel ---
		{Name: "hgetdel", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1", "f2", "v2"}},
			{Args: []string{"HGETDEL", "h", "FIELDS", "1", "f1"}},
			{Args: []string{"HEXISTS", "h", "f1"}},
			{Args: []string{"HEXISTS", "h", "f2"}},
		}},
		// --- HGetEx (get + set TTL) ---
		{Name: "hgetex with EX", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1"}},
			{Args: []string{"HGETEX", "h", "EX", "100", "FIELDS", "1", "f1"}},
			{Args: []string{"HTTL", "h", "FIELDS", "1", "f1"}, Normalize: normalizeTTLArray},
		}},
		// --- HSetEx ---
		{Name: "hsetex basic", Commands: []Command{
			{Args: []string{"HSETEX", "h", "EX", "100", "FIELDS", "1", "f1", "v1"}},
			{Args: []string{"HGET", "h", "f1"}},
			{Args: []string{"HTTL", "h", "FIELDS", "1", "f1"}, Normalize: normalizeTTLArray},
		}},
	}
}

func TestDifferentialHashTTL(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakv(t)
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range hashTTLCorpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			hayakvReplies := runScenario(t, hayakvAddr, scenario)
			redisReplies := runScenario(t, redisAddr, scenario)
			assertReplyEqual(t, scenario, hayakvReplies, redisReplies)
		})
	}
}
