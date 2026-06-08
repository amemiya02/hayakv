package diff

import (
	"strings"
	"testing"

	database "github.com/amemiya02/hayakv/internal/command"
)

var diffExclusions = map[string]string{
	"auth":       "covered by corpus_auth_test.go (standalone auth config); not in single-connection corpus",
	"bgsave":     "persistence control command; covered by persistence and integration tests",
	"blpop":      "blocking command; timeout semantics are not byte-diffable in this harness",
	"brpop":      "blocking command; timeout semantics are not byte-diffable in this harness",
	"command":    "server metadata differs from Redis 8 until command table is redesigned",
	"copyfrom":   "godis internal cluster helper command, not public Redis core command",
	"copyto":     "godis internal cluster helper command, not public Redis core command",
	"dumpkey":    "godis internal cluster helper command, not public Redis core command",
	"existin":    "godis internal cluster helper command, not public Redis core command",
	"flushall":   "covered by harness setup isolation rather than corpus command",
	"flushdb":    "covered by harness setup isolation rather than corpus command",
	"getver":     "godis internal version command, not public Redis core command",
	"info":       "contains server-specific fields that need normalization",
	"keys":       "unordered output is unsuitable for byte-for-byte corpus without normalization",
	"memory":     "MEMORY USAGE reports allocator-specific byte counts that differ from real Redis by design",
	"object":     "object introspection command; covered by the object-encoding tests",
	"publish":    "covered by corpus_pubsub_test.go (multi-connection); not in single-connection corpus",
	"randomkey":  "nondeterministic by design",
	"renamefrom": "godis internal rename helper, not public Redis core command",
	"renamenxto": "godis internal rename helper, not public Redis core command",
	"renameto":   "godis internal rename helper, not public Redis core command",
	"replconf":   "replication command; covered by integration replication tests",
	"save":       "persistence control command; covered by persistence and integration tests",
	"slaveof":    "replication command; covered by integration replication tests",
	"subscribe":  "covered by corpus_pubsub_test.go (multi-connection); not in single-connection corpus",
	// M13: Streams — stream commands use auto-ID and timing; diff corpus deferred
	"xadd":       "stream auto-ID uses wall-clock; explicit-ID diff corpus deferred",
	"xrange":     "stream range; diff corpus deferred to stream corpus file",
	"xrevrange":  "stream reverse range; diff corpus deferred to stream corpus file",
	"xlen":       "stream length; diff corpus deferred to stream corpus file",
	"xdel":       "stream delete; diff corpus deferred to stream corpus file",
	"xtrim":      "stream trim; diff corpus deferred to stream corpus file",
	"xsetid":     "stream set-id admin command; diff corpus deferred",
	"xread":      "stream read; blocking semantics not byte-diffable; non-blocking corpus deferred",
	"xgroup":     "stream consumer group management; diff corpus deferred",
	"xreadgroup": "stream consumer group read; blocking + PEL semantics not byte-diffable",
	"xack":       "stream ack; diff corpus deferred to stream corpus file",
	"xpending":   "stream pending; diff corpus deferred to stream corpus file",
	"xclaim":     "stream claim; diff corpus deferred to stream corpus file",
	"xautoclaim": "stream auto-claim; diff corpus deferred to stream corpus file",
	"xinfo":      "stream info; volatile timestamps/counts need normalization",
	// M13: HyperLogLog — HLL byte-compat being fixed; corpus deferred
	"pfadd":      "HLL add; byte-compat correction in progress; corpus deferred",
	"pfcount":    "HLL count; estimator correction in progress; corpus deferred",
	"pfmerge":    "HLL merge; corpus deferred",
	"pfdebug":    "HLL debug admin command; not in real Redis diff scope",
	"pfselftest": "HLL self-test admin command; not in real Redis diff scope",
	// M13: BITFIELD — diff corpus deferred
	"bitfield":    "bitfield operations; diff corpus deferred to bitfield corpus file",
	"bitfield_ro": "bitfield read-only; diff corpus deferred to bitfield corpus file",
	// M14: ACL — ACL user scenarios covered by corpus_auth_test.go extension
	"acl": "ACL command family; covered by acl*.tcl + auth corpus; rule-order normalization needed",
}

func TestCorpusMentionsOrExcludesEveryRegisteredCommand(t *testing.T) {
	covered := map[string]bool{}
	corpora := []func() []Scenario{baseCorpus, txnCorpus, scanCorpus, geoCorpus, variantCorpus, evalCorpus, hashTTLCorpus, keyspaceCorpus, censusCorpus, semantics8xCorpus}
	for _, corpus := range corpora {
		for _, scenario := range corpus() {
			for _, cmd := range scenario.Commands {
				if len(cmd.Args) > 0 {
					covered[strings.ToLower(cmd.Args[0])] = true
				}
			}
		}
	}

	for _, name := range database.RegisteredCommandNames() {
		name = strings.ToLower(name)
		if covered[name] {
			continue
		}
		if reason := diffExclusions[name]; reason == "" {
			t.Fatalf("registered command %q has no diff scenario or exclusion reason", name)
		}
	}
}
