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
}

func TestCorpusMentionsOrExcludesEveryRegisteredCommand(t *testing.T) {
	covered := map[string]bool{}
	corpora := []func() []Scenario{baseCorpus, txnCorpus, scanCorpus, geoCorpus, variantCorpus, evalCorpus, hashTTLCorpus, keyspaceCorpus, censusCorpus}
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
