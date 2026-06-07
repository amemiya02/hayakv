package diff

// redisDBCorpus returns scenarios that exercise SCAN and other commands
// affected by the dict implementation change.
func redisDBCorpus() []Scenario {
	return []Scenario{
		{Name: "scan empty", Commands: []Command{
			{Args: []string{"SCAN", "0"}},
		}},
		{Name: "scan with data", Commands: []Command{
			{Args: []string{"SET", "s1", "v1"}},
			{Args: []string{"SET", "s2", "v2"}},
			{Args: []string{"SET", "s3", "v3"}},
			{Args: []string{"SCAN", "0"}, Normalize: normalizeScan},
		}},
		{Name: "scan match pattern", Commands: []Command{
			{Args: []string{"SET", "user:1", "alice"}},
			{Args: []string{"SET", "user:2", "bob"}},
			{Args: []string{"SET", "item:1", "sword"}},
			{Args: []string{"SCAN", "0", "MATCH", "user:*"}, Normalize: normalizeScan},
		}},
		// scan_count removed: SCAN COUNT is a hint — different implementations
		// return different key subsets, making byte-for-byte comparison invalid.
		{Name: "dbsize", Commands: []Command{
			{Args: []string{"SET", "a", "1"}},
			{Args: []string{"SET", "b", "2"}},
			{Args: []string{"SET", "c", "3"}},
			{Args: []string{"DBSIZE"}},
		}},
		{Name: "keys pattern", Commands: []Command{
			{Args: []string{"SET", "name:1", "a"}},
			{Args: []string{"SET", "name:2", "b"}},
			{Args: []string{"SET", "other", "c"}},
			{Args: []string{"KEYS", "name:*"}, Normalize: sortRespArray},
		}},
		{Name: "flushdb then set", Commands: []Command{
			{Args: []string{"SET", "x", "1"}},
			{Args: []string{"FLUSHDB"}},
			{Args: []string{"SET", "y", "2"}},
			{Args: []string{"GET", "x"}},
			{Args: []string{"GET", "y"}},
			{Args: []string{"DBSIZE"}},
		}},
	}
}
