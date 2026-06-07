package diff

// clusterCorpus returns scenarios that exercise Redis Cluster commands:
// CLUSTER KEYSLOT, MYID, INFO, NODES, SLOTS, SHARDS, ADDSLOTS,
// ADDSLOTSRANGE, COUNTKEYSINSLOT, GETKEYSINSLOT, CROSSSLOT errors,
// and MOVED redirects.
func clusterCorpus() []Scenario {
	return []Scenario{
		// --- Pure cluster introspection (no slots needed) ---
		{Name: "cluster keyslot foo", Commands: []Command{
			{Args: []string{"CLUSTER", "KEYSLOT", "foo"}},
		}},
		{Name: "cluster keyslot hash tag", Commands: []Command{
			{Args: []string{"CLUSTER", "KEYSLOT", "{user1000}.following"}},
		}},
		{Name: "cluster keyslot empty", Commands: []Command{
			{Args: []string{"CLUSTER", "KEYSLOT", ""}},
		}},
		{Name: "cluster myid", Commands: []Command{
			{Args: []string{"CLUSTER", "MYID"}},
		}},
		{Name: "cluster info empty", Commands: []Command{
			{Args: []string{"CLUSTER", "INFO"}},
		}},
		{Name: "cluster nodes empty", Commands: []Command{
			{Args: []string{"CLUSTER", "NODES"}},
		}},
		{Name: "cluster slots empty", Commands: []Command{
			{Args: []string{"CLUSTER", "SLOTS"}},
		}},
		{Name: "cluster shards empty", Commands: []Command{
			{Args: []string{"CLUSTER", "SHARDS"}},
		}},
		// --- Slot assignment and key routing ---
		{Name: "cluster addslotsrange", Commands: []Command{
			{Args: []string{"CLUSTER", "ADDSLOTSRANGE", "0", "8191"}},
		}},
		{Name: "cluster countkeysinslot empty", Commands: []Command{
			{Args: []string{"CLUSTER", "COUNTKEYSINSLOT", "0"}},
		}},
		{Name: "cluster getkeyinslot empty", Commands: []Command{
			{Args: []string{"CLUSTER", "GETKEYSINSLOT", "0", "10"}},
		}},
	}
}
