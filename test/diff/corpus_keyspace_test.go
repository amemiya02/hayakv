package diff

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// normalizeConfigGetSortedChars normalizes a CONFIG GET reply where the
// value is a set of characters whose order is implementation-dependent
// (e.g. notify-keyspace-events flags). It sorts the characters in the
// value so byte-comparison is stable.
func normalizeConfigGetSortedChars(raw []byte) []byte {
	if len(raw) == 0 || raw[0] != '*' {
		return raw
	}
	idx := bytes.IndexByte(raw, '\r')
	if idx < 0 {
		return raw
	}
	n, err := strconv.Atoi(string(raw[1:idx]))
	if err != nil || n != 2 {
		return raw
	}
	pos := idx + 2
	// First bulk string: key name
	if pos >= len(raw) || raw[pos] != '$' {
		return raw
	}
	end := bytes.IndexByte(raw[pos:], '\r')
	if end < 0 {
		return raw
	}
	kLen, err := strconv.Atoi(string(raw[pos+1 : pos+end]))
	if err != nil {
		return raw
	}
	keyStart := pos + end + 2
	key := string(raw[keyStart : keyStart+kLen])
	pos = keyStart + kLen + 2

	// Second bulk string: value
	if pos >= len(raw) || raw[pos] != '$' {
		return raw
	}
	end = bytes.IndexByte(raw[pos:], '\r')
	if end < 0 {
		return raw
	}
	vLen, err := strconv.Atoi(string(raw[pos+1 : pos+end]))
	if err != nil {
		return raw
	}
	valStart := pos + end + 2
	val := string(raw[valStart : valStart+vLen])
	// Sort the characters
	chars := strings.Split(val, "")
	sort.Strings(chars)
	sorted := strings.Join(chars, "")

	var b bytes.Buffer
	b.WriteString("*2\r\n$")
	b.WriteString(strconv.Itoa(len(key)))
	b.WriteString("\r\n")
	b.WriteString(key)
	b.WriteString("\r\n$")
	b.WriteString(strconv.Itoa(len(sorted)))
	b.WriteString("\r\n")
	b.WriteString(sorted)
	b.WriteString("\r\n")
	return b.Bytes()
}

// keyspaceCorpus covers the keyspace-notification config surface.
// Actual async pub/sub notification delivery requires multi-connection
// with non-deterministic timing, so that is covered by integration tests
// rather than byte-diff.
func keyspaceCorpus() []Scenario {
	return []Scenario{
		{Name: "keyspace config set/get", Commands: []Command{
			{Args: []string{"CONFIG", "SET", "notify-keyspace-events", "KEA"}},
			{Args: []string{"CONFIG", "GET", "notify-keyspace-events"}, Normalize: normalizeConfigGetSortedChars},
			{Args: []string{"CONFIG", "SET", "notify-keyspace-events", ""}},
			{Args: []string{"CONFIG", "GET", "notify-keyspace-events"}, Normalize: normalizeConfigGetSortedChars},
		}},
	}
}

func TestDifferentialKeyspace(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakv(t)
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range keyspaceCorpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			hayakvReplies := runScenario(t, hayakvAddr, scenario)
			redisReplies := runScenario(t, redisAddr, scenario)
			assertReplyEqual(t, scenario, hayakvReplies, redisReplies)
		})
	}
}
