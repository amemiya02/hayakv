package diff

import (
	"bytes"
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/digest"
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
		// SET IFEQ — conditional write: set only if current value matches
		{Name: "set_ifeq_matching", Commands: []Command{
			{Args: []string{"SET", "k", "v1"}},
			{Args: []string{"SET", "k", "v2", "IFEQ", "v1"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set_ifeq_non-matching", Commands: []Command{
			{Args: []string{"SET", "k", "v1"}},
			{Args: []string{"SET", "k", "v2", "IFEQ", "nope"}},
			{Args: []string{"GET", "k"}},
		}},

		// SET IFNE — conditional write: set if key absent OR value differs
		{Name: "set_ifne_absent", Commands: []Command{
			{Args: []string{"DEL", "k"}},
			{Args: []string{"SET", "k", "v1", "IFNE", "v1"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set_ifne_differing", Commands: []Command{
			{Args: []string{"SET", "k", "old"}},
			{Args: []string{"SET", "k", "new", "IFNE", "xxx"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set_ifne_matching", Commands: []Command{
			{Args: []string{"SET", "k", "v1"}},
			{Args: []string{"SET", "k", "v2", "IFNE", "v1"}},
			{Args: []string{"GET", "k"}},
		}},

		// DELEX — single-key conditional delete
		// DELEX key: delete if exists, return 1; else return 0
		{Name: "delex_existing", Commands: []Command{
			{Args: []string{"SET", "a", "1"}},
			{Args: []string{"DELEX", "a"}},
		}},
		{Name: "delex_non_existing", Commands: []Command{
			{Args: []string{"DELEX", "nokey"}},
		}},
		// DELEX key IFEQ value: delete only if current == value
		{Name: "delex_ifeq_matching", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFEQ", "hello"}},
		}},
		{Name: "delex_ifeq_non_matching", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFEQ", "world"}},
			{Args: []string{"GET", "a"}},
		}},
		// DELEX key IFNE value: delete if key absent OR value differs
		{Name: "delex_ifne_differing", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFNE", "world"}},
		}},
		{Name: "delex_ifne_matching", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFNE", "hello"}},
			{Args: []string{"GET", "a"}},
		}},
		// DELEX with invalid condition
		{Name: "delex_bad_condition", Commands: []Command{
			{Args: []string{"SET", "a", "1"}},
			{Args: []string{"DELEX", "a", "BADCMD", "x"}},
		}},
		// DELEX IFDEQ/IFDNE — digest-based conditional delete
		{Name: "delex_ifdeq_matching", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFDEQ", digest.ValueDigest([]byte("hello"))}},
		}},
		{Name: "delex_ifdeq_non_matching", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFDEQ", digest.ValueDigest([]byte("wrong"))}},
			{Args: []string{"GET", "a"}},
		}},
		{Name: "delex_ifdne_differing", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFDNE", digest.ValueDigest([]byte("other"))}},
		}},
		{Name: "delex_ifdne_matching", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"DELEX", "a", "IFDNE", digest.ValueDigest([]byte("hello"))}},
			{Args: []string{"GET", "a"}},
		}},

		// SET IFDEQ/IFDNE — digest-based conditional write
		{Name: "set_ifdeq_matching", Commands: []Command{
			{Args: []string{"SET", "k", "hello"}},
			{Args: []string{"SET", "k", "world", "IFDEQ", digest.ValueDigest([]byte("hello"))}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set_ifdeq_non_matching", Commands: []Command{
			{Args: []string{"SET", "k", "hello"}},
			{Args: []string{"SET", "k", "world", "IFDEQ", digest.ValueDigest([]byte("wrong"))}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set_ifdne_absent", Commands: []Command{
			{Args: []string{"DEL", "k"}},
			{Args: []string{"SET", "k", "val", "IFDNE", digest.ValueDigest([]byte("anything"))}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set_ifdne_differing", Commands: []Command{
			{Args: []string{"SET", "k", "hello"}},
			{Args: []string{"SET", "k", "world", "IFDNE", digest.ValueDigest([]byte("other"))}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "set_ifdne_matching", Commands: []Command{
			{Args: []string{"SET", "k", "hello"}},
			{Args: []string{"SET", "k", "world", "IFDNE", digest.ValueDigest([]byte("hello"))}},
			{Args: []string{"GET", "k"}},
		}},

		// MSETEX numkeys k v [k v…] [EX s]
		// First arg is numkeys, expiry is optional
		{Name: "msetex_basic", Commands: []Command{
			{Args: []string{"MSETEX", "2", "x", "1", "y", "2"}},
			{Args: []string{"GET", "x"}},
			{Args: []string{"GET", "y"}},
		}},
		{Name: "msetex_with_ex", Commands: []Command{
			{Args: []string{"MSETEX", "2", "x", "1", "y", "2", "EX", "100"}},
			{Args: []string{"GET", "x"}},
			{Args: []string{"GET", "y"}},
			{Args: []string{"TTL", "x"}, Normalize: normalizeTTL},
			{Args: []string{"TTL", "y"}, Normalize: normalizeTTL},
		}},

		// BITOP DIFF — bits in first key only
		{Name: "bitop_diff", Commands: []Command{
			{Args: []string{"SET", "a", "abc"}},
			{Args: []string{"SET", "b", "abd"}},
			{Args: []string{"BITOP", "DIFF", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},

		// BITOP AND/OR/XOR/NOT — basic operations
		{Name: "bitop_and", Commands: []Command{
			{Args: []string{"SET", "a", "\xff\xff"}},
			{Args: []string{"SET", "b", "\x0f\x0f"}},
			{Args: []string{"BITOP", "AND", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},
		{Name: "bitop_or", Commands: []Command{
			{Args: []string{"SET", "a", "\xff\x00"}},
			{Args: []string{"SET", "b", "\x00\xff"}},
			{Args: []string{"BITOP", "OR", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},
		{Name: "bitop_xor", Commands: []Command{
			{Args: []string{"SET", "a", "\xff\xff"}},
			{Args: []string{"SET", "b", "\x0f\xf0"}},
			{Args: []string{"BITOP", "XOR", "d", "a", "b"}},
			{Args: []string{"GET", "d"}},
		}},
		{Name: "bitop_not", Commands: []Command{
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
