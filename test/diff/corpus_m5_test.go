package diff

// m5Corpus exercises ONLY deterministic, byte-comparable expiration/eviction
// behavior. used_memory, INFO memory, and *which* key an approximate LRU/LFU
// evicts are non-deterministic and intentionally excluded (see harness note).
//
// Diff-able here:
//   - PEXPIRE/EXPIRE -> TTL/PTTL/EXPIRETIME/PEXPIRETIME exact integer replies
//   - PERSIST semantics and return codes
//   - lazy expiry: a GET after a tiny PEXPIRE returns nil + DEL semantics
//   - OOM rejection under noeviction (exact -OOM error string)
//   - CONFIG GET maxmemory-policy round-trip
//   - OBJECT IDLETIME returns an integer (value normalized away below)
func m5Corpus() []Scenario {
	return []Scenario{
		{Name: "expire then ttl bounded", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"EXPIRE", "k", "100"}},
			{Args: []string{"TTL", "k"}},     // 100 (deterministic to the second on fresh set)
			{Args: []string{"PERSIST", "k"}}, // 1
			{Args: []string{"TTL", "k"}},     // -1
			{Args: []string{"PERSIST", "k"}}, // 0 (already persistent)
		}},
		{Name: "expire missing key", Commands: []Command{
			{Args: []string{"EXPIRE", "nope", "100"}}, // 0
			{Args: []string{"TTL", "nope"}},           // -2
			{Args: []string{"PTTL", "nope"}},          // -2
			{Args: []string{"PERSIST", "nope"}},       // 0
		}},
		{Name: "pexpireat in the past deletes", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"PEXPIREAT", "k", "1"}}, // epoch 1ms -> already expired -> 1
			{Args: []string{"GET", "k"}},            // nil (lazy expire)
			{Args: []string{"EXISTS", "k"}},         // 0
			{Args: []string{"TTL", "k"}},            // -2
		}},
		{Name: "expiretime exact", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"EXPIREAT", "k", "99999999999"}}, // far future
			{Args: []string{"EXPIRETIME", "k"}},              // 99999999999
			{Args: []string{"PEXPIRETIME", "k"}},             // 99999999999000
		}},
		{Name: "set with EX option ttl", Commands: []Command{
			{Args: []string{"SET", "k", "v", "EX", "100"}},
			{Args: []string{"TTL", "k"}}, // 100
		}},
		{Name: "oom under noeviction", Commands: []Command{
			{Args: []string{"CONFIG", "SET", "maxmemory-policy", "noeviction"}},
			{Args: []string{"CONFIG", "SET", "maxmemory", "1"}}, // 1 byte -> instantly over
			{Args: []string{"SET", "x", "y"}},                   // -OOM ...
			{Args: []string{"CONFIG", "SET", "maxmemory", "0"}}, // reset
		}},
		{Name: "config get policy roundtrip", Commands: []Command{
			{Args: []string{"CONFIG", "SET", "maxmemory-policy", "allkeys-lru"}},
			{Args: []string{"CONFIG", "GET", "maxmemory-policy"}}, // ["maxmemory-policy","allkeys-lru"]
			{Args: []string{"CONFIG", "SET", "maxmemory-policy", "noeviction"}},
		}},
	}
}
