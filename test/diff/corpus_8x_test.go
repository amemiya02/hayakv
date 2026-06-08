package diff

import (
	"bytes"
	"testing"
)

// normalizeTTL clamps a positive integer reply to "1" so that
// near-simultaneous TTL values don't cause false diff failures.
func normalizeTTL(raw []byte) []byte {
	if len(raw) == 0 || raw[0] != ':' {
		return raw
	}
	// positive TTL → ":1\r\n"; zero or negative → unchanged
	idx := bytes.IndexByte(raw, '\r')
	if idx < 0 {
		return raw
	}
	s := string(raw[1:idx])
	if s == "0" || s == "-1" || s == "-2" {
		return raw
	}
	return []byte(":1\r\n")
}

func semantics8xCorpus() []Scenario {
	return []Scenario{
		// SET IFEQ — conditional write based on current value
		{Name: "set ifeq matching", Commands: []Command{
			{Args: []string{"SET", "k", "v1"}},
			{Args: []string{"SET", "k", "v2", "IFEQ", "v1"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set ifeq non-matching", Commands: []Command{
			{Args: []string{"SET", "k", "v1"}},
			{Args: []string{"SET", "k", "v2", "IFEQ", "nope"}},
			{Args: []string{"GET", "k"}},
		}},

		// DELEX — delete keys returning count
		{Name: "delex existing", Commands: []Command{
			{Args: []string{"MSET", "a", "1", "b", "2"}},
			{Args: []string{"DELEX", "a", "b", "c"}},
		}},
		{Name: "delex none existing", Commands: []Command{
			{Args: []string{"DELEX", "x", "y"}},
		}},

		// MSETEX — multiple set with shared TTL
		{Name: "msetex basic", Commands: []Command{
			{Args: []string{"MSETEX", "100", "x", "1", "y", "2"}},
			{Args: []string{"GET", "x"}},
			{Args: []string{"GET", "y"}},
			{Args: []string{"TTL", "x"}, Normalize: normalizeTTL},
			{Args: []string{"TTL", "y"}, Normalize: normalizeTTL},
		}},

		// BITOP DIFF — bits in first key only
		{Name: "bitop diff", Commands: []Command{
			{Args: []string{"SET", "a", "abc"}},
			{Args: []string{"SET", "b", "abd"}},
			{Args: []string{"BITOP", "DIFF", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},

		// BITOP AND/OR/XOR/NOT — basic operations
		{Name: "bitop and", Commands: []Command{
			{Args: []string{"SET", "a", "\xff\xff"}},
			{Args: []string{"SET", "b", "\x0f\x0f"}},
			{Args: []string{"BITOP", "AND", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},
		{Name: "bitop or", Commands: []Command{
			{Args: []string{"SET", "a", "\xff\x00"}},
			{Args: []string{"SET", "b", "\x00\xff"}},
			{Args: []string{"BITOP", "OR", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},
		{Name: "bitop xor", Commands: []Command{
			{Args: []string{"SET", "a", "\xff\xff"}},
			{Args: []string{"SET", "b", "\x0f\xf0"}},
			{Args: []string{"BITOP", "XOR", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},
		{Name: "bitop not", Commands: []Command{
			{Args: []string{"SET", "a", "\xff\x00"}},
			{Args: []string{"BITOP", "NOT", "d", "a"}},
			{Args: []string{"GET", "d"}},
		}},
	}
}

func TestDifferential8x(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakv(t)
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range semantics8xCorpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			hayakvReplies := runScenario(t, hayakvAddr, scenario)
			redisReplies := runScenario(t, redisAddr, scenario)
			assertReplyEqual(t, scenario, hayakvReplies, redisReplies)
		})
	}
}
