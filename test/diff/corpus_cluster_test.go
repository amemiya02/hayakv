package diff

// clusterCorpus exercises the deterministic, node-identity-independent slice of
// the Redis Cluster command surface: CLUSTER KEYSLOT. KEYSLOT is pure
// CRC16(key) mod 16384 with hash-tag extraction, so its reply is byte-for-byte
// identical between hayakv and real Redis regardless of node id, port, epoch or
// slot assignment. It is run by TestDifferentialCluster (see
// harness_cluster_test.go), which spins up cluster-enabled servers on both
// sides.
//
// The introspection and slot-management commands (CLUSTER MYID / NODES / INFO /
// SLOTS / SHARDS / ADDSLOTSRANGE / COUNTKEYSINSLOT / GETKEYSINSLOT) are
// intentionally excluded from byte-for-byte diffing: their replies embed random
// node ids, instance-specific ports, and epochs that differ between any two
// servers. They are covered by the internal/rediscluster unit tests instead.
func clusterCorpus() []Scenario {
	return []Scenario{
		{Name: "keyslot plain keys", Commands: []Command{
			{Args: []string{"CLUSTER", "KEYSLOT", "foo"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "bar"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "baz"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "123456789"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "Hello World"}},
		}},
		{Name: "keyslot empty key", Commands: []Command{
			{Args: []string{"CLUSTER", "KEYSLOT", ""}},
		}},
		{Name: "keyslot hash tag", Commands: []Command{
			{Args: []string{"CLUSTER", "KEYSLOT", "{user1000}.following"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "{user1000}.followers"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "a{b}c"}},
		}},
		{Name: "keyslot hash tag edge cases", Commands: []Command{
			// Empty tag "{}" hashes the whole key.
			{Args: []string{"CLUSTER", "KEYSLOT", "{}foo"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "foo{}{bar}"}},
			// First non-empty "{...}" wins.
			{Args: []string{"CLUSTER", "KEYSLOT", "x{}{y}"}},
		}},
		{Name: "keyslot hash tag equivalence", Commands: []Command{
			// All three map to the slot of "u".
			{Args: []string{"CLUSTER", "KEYSLOT", "{u}.a"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "{u}.b"}},
			{Args: []string{"CLUSTER", "KEYSLOT", "{u}"}},
		}},
	}
}
