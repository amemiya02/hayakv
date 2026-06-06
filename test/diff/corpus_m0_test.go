package diff

type Command struct {
	Args []string
}

type Scenario struct {
	Name     string
	Commands []Command
}

func m0Corpus() []Scenario {
	return []Scenario{
		{Name: "ping", Commands: []Command{{Args: []string{"PING"}}}},
		{Name: "string set get del", Commands: []Command{
			{Args: []string{"SET", "s", "v"}},
			{Args: []string{"GET", "s"}},
			{Args: []string{"DEL", "s"}},
			{Args: []string{"GET", "s"}},
		}},
		{Name: "string numeric", Commands: []Command{
			{Args: []string{"SET", "n", "1"}},
			{Args: []string{"INCR", "n"}},
			{Args: []string{"DECRBY", "n", "2"}},
			{Args: []string{"GET", "n"}},
		}},
		{Name: "hash", Commands: []Command{
			{Args: []string{"HSET", "h", "f", "v"}},
			{Args: []string{"HGET", "h", "f"}},
			{Args: []string{"HLEN", "h"}},
			{Args: []string{"HDEL", "h", "f"}},
		}},
		{Name: "list", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "b", "c"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
			{Args: []string{"LPOP", "l"}},
			{Args: []string{"LLEN", "l"}},
		}},
		{Name: "set sorted response", Commands: []Command{
			{Args: []string{"SADD", "set", "a", "b"}},
			{Args: []string{"SISMEMBER", "set", "a"}},
			{Args: []string{"SCARD", "set"}},
			{Args: []string{"SREM", "set", "a"}},
		}},
		{Name: "zset", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b"}},
			{Args: []string{"ZRANGE", "z", "0", "-1"}},
			{Args: []string{"ZSCORE", "z", "a"}},
			{Args: []string{"ZREM", "z", "a"}},
		}},
		{Name: "keys ttl", Commands: []Command{
			{Args: []string{"SET", "ttl", "v"}},
			{Args: []string{"EXPIRE", "ttl", "600"}},
			{Args: []string{"PERSIST", "ttl"}},
			{Args: []string{"TTL", "ttl"}},
		}},
		{Name: "wrong type", Commands: []Command{
			{Args: []string{"SET", "wrong", "v"}},
			{Args: []string{"LPUSH", "wrong", "x"}},
		}},
		{Name: "arity error", Commands: []Command{
			{Args: []string{"GET"}},
			{Args: []string{"SET", "only-key"}},
		}},
	}
}
