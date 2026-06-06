package diff

// m1Corpus exercises RESP3-visible behaviors. The runner prepends HELLO 3.
func m1Corpus() []Scenario {
	return []Scenario{
		{Name: "resp3 get miss is null", Commands: []Command{
			{Args: []string{"GET", "nope"}},
		}},
		{Name: "resp3 hgetall is map", Commands: []Command{
			{Args: []string{"HSET", "h", "f1", "v1", "f2", "v2"}},
			{Args: []string{"HGETALL", "h"}},
		}},
		{Name: "resp3 incr is int", Commands: []Command{
			{Args: []string{"SET", "n", "1"}},
			{Args: []string{"INCR", "n"}},
		}},
		{Name: "resp3 exists", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"EXISTS", "k"}},
		}},
	}
}
