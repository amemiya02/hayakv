package diff

import "testing"

// variantCorpus covers command variants that share code paths with baseCorpus
// commands but were previously excluded from the diff gate. Each scenario
// exercises a specific variant so the coverage gate can shrink diffExclusions.

func variantCorpus() []Scenario {
	return []Scenario{
		// --- string variants ---
		{Name: "append", Commands: []Command{
			{Args: []string{"SET", "a", "hello"}},
			{Args: []string{"APPEND", "a", " world"}},
			{Args: []string{"GET", "a"}},
		}},
		{Name: "append_encoding", Commands: []Command{
			{Args: []string{"SET", "ae", "hello"}},
			{Args: []string{"OBJECT", "ENCODING", "ae"}},
			{Args: []string{"APPEND", "ae", "world"}},
			{Args: []string{"OBJECT", "ENCODING", "ae"}},
			{Args: []string{"APPEND", "ae2", "x"}},
			{Args: []string{"OBJECT", "ENCODING", "ae2"}},
			{Args: []string{"SET", "ae3", "123"}},
			{Args: []string{"APPEND", "ae3", "4"}},
			{Args: []string{"OBJECT", "ENCODING", "ae3"}},
			{Args: []string{"GET", "ae3"}},
		}},
		{Name: "setrange_encoding", Commands: []Command{
			{Args: []string{"SET", "sr", "hello"}},
			{Args: []string{"SETRANGE", "sr", "0", "H"}},
			{Args: []string{"OBJECT", "ENCODING", "sr"}},
			{Args: []string{"SETRANGE", "sr2", "0", "hi"}},
			{Args: []string{"OBJECT", "ENCODING", "sr2"}},
		}},
		{Name: "setrange_empty_value", Commands: []Command{
			{Args: []string{"SETRANGE", "sre", "0", ""}},
			{Args: []string{"EXISTS", "sre"}},
			{Args: []string{"SETRANGE", "sre", "5", ""}},
			{Args: []string{"EXISTS", "sre"}},
			{Args: []string{"SET", "sre2", "hello"}},
			{Args: []string{"SETRANGE", "sre2", "0", ""}},
			{Args: []string{"OBJECT", "ENCODING", "sre2"}},
		}},
		{Name: "encoding_value_thresholds", Commands: []Command{
			{Args: []string{"HSET", "evh", "f", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}},
			{Args: []string{"OBJECT", "ENCODING", "evh"}},
			{Args: []string{"ZADD", "evz", "1", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}},
			{Args: []string{"OBJECT", "ENCODING", "evz"}},
			{Args: []string{"SADD", "evs", "a"}},
			{Args: []string{"SADD", "evs", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}},
			{Args: []string{"OBJECT", "ENCODING", "evs"}},
			{Args: []string{"RPUSH", "evl", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}},
			{Args: []string{"OBJECT", "ENCODING", "evl"}},
		}},
		{Name: "config_set_encoding_threshold", Commands: []Command{
			{Args: []string{"CONFIG", "SET", "hash-max-listpack-entries", "4"}},
			{Args: []string{"CONFIG", "GET", "hash-max-listpack-entries"}},
			{Args: []string{"HSET", "cst", "a", "1", "b", "2", "c", "3", "d", "4", "e", "5"}},
			{Args: []string{"OBJECT", "ENCODING", "cst"}},
			{Args: []string{"CONFIG", "SET", "hash-max-listpack-entries", "128"}},
			{Args: []string{"HSET", "cst2", "a", "1", "b", "2", "c", "3", "d", "4", "e", "5"}},
			{Args: []string{"OBJECT", "ENCODING", "cst2"}},
		}},
		{Name: "bit_write_encoding", Commands: []Command{
			{Args: []string{"SETBIT", "bw", "7", "1"}},
			{Args: []string{"OBJECT", "ENCODING", "bw"}},
			{Args: []string{"SET", "bw2", "hello"}},
			{Args: []string{"SETBIT", "bw2", "0", "1"}},
			{Args: []string{"OBJECT", "ENCODING", "bw2"}},
			{Args: []string{"BITFIELD", "bw3", "SET", "u8", "0", "255"}},
			{Args: []string{"OBJECT", "ENCODING", "bw3"}},
			{Args: []string{"BITOP", "AND", "bw4", "bw", "bw2"}},
			{Args: []string{"OBJECT", "ENCODING", "bw4"}},
		}},
		{Name: "decr", Commands: []Command{
			{Args: []string{"SET", "n", "10"}},
			{Args: []string{"DECR", "n"}},
			{Args: []string{"GET", "n"}},
		}},
		{Name: "incrby", Commands: []Command{
			{Args: []string{"SET", "n", "1"}},
			{Args: []string{"INCRBY", "n", "5"}},
			{Args: []string{"GET", "n"}},
		}},
		{Name: "incrbyfloat", Commands: []Command{
			{Args: []string{"SET", "n", "1.5"}},
			{Args: []string{"INCRBYFLOAT", "n", "2.5"}},
		}},
		{Name: "decrby", Commands: []Command{
			{Args: []string{"SET", "n", "10"}},
			{Args: []string{"DECRBY", "n", "3"}},
			{Args: []string{"GET", "n"}},
		}},
		{Name: "setex", Commands: []Command{
			{Args: []string{"SETEX", "k", "600", "v"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "setnx", Commands: []Command{
			{Args: []string{"SETNX", "k", "v"}},
			{Args: []string{"SETNX", "k", "v2"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "psetex", Commands: []Command{
			{Args: []string{"PSETEX", "k", "600000", "v"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "getrange", Commands: []Command{
			{Args: []string{"SET", "k", "hello world"}},
			{Args: []string{"GETRANGE", "k", "0", "4"}},
		}},
		{Name: "setrange", Commands: []Command{
			{Args: []string{"SET", "k", "hello world"}},
			{Args: []string{"SETRANGE", "k", "6", "redis"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "strlen", Commands: []Command{
			{Args: []string{"SET", "k", "hello"}},
			{Args: []string{"STRLEN", "k"}},
		}},
		{Name: "getset", Commands: []Command{
			{Args: []string{"SET", "k", "old"}},
			{Args: []string{"GETSET", "k", "new"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "getdel", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"GETDEL", "k"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "getex", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"GETEX", "k", "EX", "600"}},
		}},
		{Name: "mget", Commands: []Command{
			{Args: []string{"SET", "a", "1"}},
			{Args: []string{"SET", "b", "2"}},
			{Args: []string{"MGET", "a", "b", "c"}},
		}},
		{Name: "mset", Commands: []Command{
			{Args: []string{"MSET", "a", "1", "b", "2"}},
			{Args: []string{"MGET", "a", "b"}},
		}},
		{Name: "msetnx", Commands: []Command{
			{Args: []string{"MSETNX", "a", "1", "b", "2"}},
			{Args: []string{"MSETNX", "b", "3", "c", "4"}},
			{Args: []string{"MGET", "a", "b", "c"}},
		}},
		{Name: "exists", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"EXISTS", "k", "missing"}},
		}},
		{Name: "rename", Commands: []Command{
			{Args: []string{"SET", "a", "v"}},
			{Args: []string{"RENAME", "a", "b"}},
			{Args: []string{"GET", "b"}},
		}},
		{Name: "renamenx", Commands: []Command{
			{Args: []string{"SET", "a", "v"}},
			{Args: []string{"SET", "b", "w"}},
			{Args: []string{"RENAMENX", "a", "b"}},
			{Args: []string{"RENAMENX", "a", "c"}},
		}},
		{Name: "type", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"TYPE", "k"}},
			{Args: []string{"TYPE", "missing"}},
		}},
		// --- hash variants ---
		{Name: "hexists", Commands: []Command{
			{Args: []string{"HSET", "h", "f", "v"}},
			{Args: []string{"HEXISTS", "h", "f"}},
			{Args: []string{"HEXISTS", "h", "missing"}},
		}},
		{Name: "hgetall", Commands: []Command{
			{Args: []string{"HSET", "h", "a", "1", "b", "2"}},
			{Args: []string{"HGETALL", "h"}},
		}},
		{Name: "hincrby", Commands: []Command{
			{Args: []string{"HSET", "h", "n", "10"}},
			{Args: []string{"HINCRBY", "h", "n", "5"}},
		}},
		{Name: "hincrbyfloat", Commands: []Command{
			{Args: []string{"HSET", "h", "n", "1.5"}},
			{Args: []string{"HINCRBYFLOAT", "h", "n", "2.5"}},
		}},
		{Name: "hkeys", Commands: []Command{
			{Args: []string{"HSET", "h", "a", "1", "b", "2"}},
			{Args: []string{"HKEYS", "h"}},
		}},
		{Name: "hmget", Commands: []Command{
			{Args: []string{"HSET", "h", "a", "1", "b", "2"}},
			{Args: []string{"HMGET", "h", "a", "b", "c"}},
		}},
		{Name: "hmset", Commands: []Command{
			{Args: []string{"HMSET", "h", "a", "1", "b", "2"}},
			{Args: []string{"HGETALL", "h"}},
		}},
		{Name: "hrandfield", Commands: []Command{
			{Args: []string{"HSET", "h", "a", "1", "b", "2", "c", "3"}},
			{Args: []string{"HRANDFIELD", "h", "2"}},
		}},
		{Name: "hsetnx", Commands: []Command{
			{Args: []string{"HSETNX", "h", "f", "v"}},
			{Args: []string{"HSETNX", "h", "f", "v2"}},
			{Args: []string{"HGET", "h", "f"}},
		}},
		{Name: "hstrlen", Commands: []Command{
			{Args: []string{"HSET", "h", "f", "hello"}},
			{Args: []string{"HSTRLEN", "h", "f"}},
		}},
		{Name: "hvals", Commands: []Command{
			{Args: []string{"HSET", "h", "a", "1", "b", "2"}},
			{Args: []string{"HVALS", "h"}},
		}},
		// --- list variants ---
		{Name: "lindex", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "b", "c"}},
			{Args: []string{"LINDEX", "l", "1"}},
		}},
		{Name: "linsert", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "c"}},
			{Args: []string{"LINSERT", "l", "BEFORE", "c", "b"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
		}},
		{Name: "lpushx", Commands: []Command{
			{Args: []string{"LPUSHX", "l", "a"}},
			{Args: []string{"RPUSH", "l", "b"}},
			{Args: []string{"LPUSHX", "l", "a"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
		}},
		{Name: "lrem", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "b", "a", "c", "a"}},
			{Args: []string{"LREM", "l", "2", "a"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
		}},
		{Name: "lset", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "b", "c"}},
			{Args: []string{"LSET", "l", "1", "x"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
		}},
		{Name: "ltrim", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "b", "c", "d"}},
			{Args: []string{"LTRIM", "l", "1", "2"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
		}},
		{Name: "rpop", Commands: []Command{
			{Args: []string{"RPUSH", "l", "a", "b", "c"}},
			{Args: []string{"RPOP", "l"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
		}},
		{Name: "rpoplpush", Commands: []Command{
			{Args: []string{"RPUSH", "l1", "a", "b"}},
			{Args: []string{"RPUSH", "l2", "c"}},
			{Args: []string{"RPOPLPUSH", "l1", "l2"}},
			{Args: []string{"LRANGE", "l1", "0", "-1"}},
			{Args: []string{"LRANGE", "l2", "0", "-1"}},
		}},
		{Name: "rpushx", Commands: []Command{
			{Args: []string{"RPUSHX", "l", "a"}},
			{Args: []string{"RPUSH", "l", "b"}},
			{Args: []string{"RPUSHX", "l", "c"}},
			{Args: []string{"LRANGE", "l", "0", "-1"}},
		}},
		// --- set variants ---
		{Name: "sdiff", Commands: []Command{
			{Args: []string{"SADD", "s1", "a", "b", "c"}},
			{Args: []string{"SADD", "s2", "b", "c", "d"}},
			{Args: []string{"SDIFF", "s1", "s2"}, Normalize: sortRespArray},
		}},
		{Name: "sdiffstore", Commands: []Command{
			{Args: []string{"SADD", "s1", "a", "b", "c"}},
			{Args: []string{"SADD", "s2", "b", "c", "d"}},
			{Args: []string{"SDIFFSTORE", "s3", "s1", "s2"}},
			{Args: []string{"SMEMBERS", "s3"}, Normalize: sortRespArray},
		}},
		{Name: "sinter", Commands: []Command{
			{Args: []string{"SADD", "s1", "a", "b", "c"}},
			{Args: []string{"SADD", "s2", "b", "c", "d"}},
			{Args: []string{"SINTER", "s1", "s2"}, Normalize: sortRespArray},
		}},
		{Name: "sinterstore", Commands: []Command{
			{Args: []string{"SADD", "s1", "a", "b", "c"}},
			{Args: []string{"SADD", "s2", "b", "c", "d"}},
			{Args: []string{"SINTERSTORE", "s3", "s1", "s2"}},
			{Args: []string{"SMEMBERS", "s3"}, Normalize: sortRespArray},
		}},
		{Name: "smembers", Commands: []Command{
			{Args: []string{"SADD", "s", "a", "b", "c"}},
			{Args: []string{"SMEMBERS", "s"}, Normalize: sortRespArray},
		}},
		{Name: "spop", Commands: []Command{
			{Args: []string{"SADD", "s", "a"}},
			{Args: []string{"SPOP", "s"}},
		}},
		{Name: "srandmember", Commands: []Command{
			{Args: []string{"SADD", "s", "a", "b", "c"}},
			{Args: []string{"SRANDMEMBER", "s", "2"}},
		}},
		{Name: "sunion", Commands: []Command{
			{Args: []string{"SADD", "s1", "a", "b"}},
			{Args: []string{"SADD", "s2", "b", "c"}},
			{Args: []string{"SUNION", "s1", "s2"}, Normalize: sortRespArray},
		}},
		{Name: "sunionstore", Commands: []Command{
			{Args: []string{"SADD", "s1", "a", "b"}},
			{Args: []string{"SADD", "s2", "b", "c"}},
			{Args: []string{"SUNIONSTORE", "s3", "s1", "s2"}},
			{Args: []string{"SMEMBERS", "s3"}, Normalize: sortRespArray},
		}},
		// --- zset variants ---
		{Name: "zcard", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b"}},
			{Args: []string{"ZCARD", "z"}},
		}},
		{Name: "zcount", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZCOUNT", "z", "1", "2"}},
		}},
		{Name: "zincrby", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a"}},
			{Args: []string{"ZINCRBY", "z", "5", "a"}},
			{Args: []string{"ZSCORE", "z", "a"}},
		}},
		{Name: "zlexcount", Commands: []Command{
			{Args: []string{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d"}},
			{Args: []string{"ZLEXCOUNT", "z", "[b", "[d"}},
		}},
		{Name: "zpopmin", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b"}},
			{Args: []string{"ZPOPMIN", "z"}},
		}},
		{Name: "zrangebylex", Commands: []Command{
			{Args: []string{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d"}},
			{Args: []string{"ZRANGEBYLEX", "z", "[b", "[d"}},
		}},
		{Name: "zrangebyscore", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZRANGEBYSCORE", "z", "1", "2"}},
		}},
		{Name: "zrank", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZRANK", "z", "b"}},
		}},
		{Name: "zremrangebylex", Commands: []Command{
			{Args: []string{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d"}},
			{Args: []string{"ZREMRANGEBYLEX", "z", "[b", "[d"}},
			{Args: []string{"ZRANGE", "z", "0", "-1"}},
		}},
		{Name: "zremrangebyrank", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZREMRANGEBYRANK", "z", "0", "1"}},
			{Args: []string{"ZRANGE", "z", "0", "-1"}},
		}},
		{Name: "zremrangebyscore", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZREMRANGEBYSCORE", "z", "1", "2"}},
			{Args: []string{"ZRANGE", "z", "0", "-1"}},
		}},
		{Name: "zrevrange", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZREVRANGE", "z", "0", "-1"}},
		}},
		{Name: "zrevrangebylex", Commands: []Command{
			{Args: []string{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d"}},
			{Args: []string{"ZREVRANGEBYLEX", "z", "[d", "[b"}},
		}},
		{Name: "zrevrangebyscore", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZREVRANGEBYSCORE", "z", "2", "1"}},
		}},
		{Name: "zrevrank", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZREVRANK", "z", "b"}},
		}},
		// --- expiry variants ---
		{Name: "expireat", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"EXPIREAT", "k", "2000000000"}},
			{Args: []string{"TTL", "k"}},
		}},
		{Name: "expiretime", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"EXPIRE", "k", "600"}},
			{Args: []string{"EXPIRETIME", "k"}},
		}},
		{Name: "pexpire", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"PEXPIRE", "k", "600000"}},
			{Args: []string{"PTTL", "k"}},
		}},
		{Name: "pexpireat", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"PEXPIREAT", "k", "2000000000000"}},
			{Args: []string{"PTTL", "k"}},
		}},
		{Name: "pexpiretime", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"PEXPIRE", "k", "600000"}},
			{Args: []string{"PEXPIRETIME", "k"}},
		}},
		{Name: "pttl", Commands: []Command{
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"PEXPIRE", "k", "600000"}},
			{Args: []string{"PTTL", "k"}},
		}},
		// --- bit variants ---
		{Name: "getbit", Commands: []Command{
			{Args: []string{"SETBIT", "b", "7", "1"}},
			{Args: []string{"GETBIT", "b", "7"}},
			{Args: []string{"GETBIT", "b", "0"}},
		}},
		{Name: "setbit", Commands: []Command{
			{Args: []string{"SETBIT", "b", "7", "1"}},
			{Args: []string{"GET", "b"}},
		}},
		{Name: "bitcount", Commands: []Command{
			{Args: []string{"SET", "b", "foobar"}},
			{Args: []string{"BITCOUNT", "b"}},
		}},
		{Name: "bitpos", Commands: []Command{
			{Args: []string{"SET", "b", "\xff\xf0\x00"}},
			{Args: []string{"BITPOS", "b", "0"}},
		}},
		// --- scan variants ---
		{Name: "hscan", Commands: []Command{
			{Args: []string{"HSET", "h", "a", "1", "b", "2", "c", "3"}},
			{Args: []string{"HSCAN", "h", "0", "COUNT", "1000"}},
		}},
		{Name: "sscan", Commands: []Command{
			{Args: []string{"SADD", "s", "a", "b", "c"}},
			{Args: []string{"SSCAN", "s", "0", "COUNT", "1000"}},
		}},
		{Name: "zscan", Commands: []Command{
			{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
			{Args: []string{"ZSCAN", "z", "0", "COUNT", "1000"}},
		}},
		// --- transaction variant ---
		{Name: "watch", Commands: []Command{
			{Args: []string{"WATCH", "k"}},
			{Args: []string{"MULTI"}},
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"EXEC"}},
		}},
		// --- select ---
		{Name: "select", Commands: []Command{
			{Args: []string{"SELECT", "1"}},
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"SELECT", "0"}},
			{Args: []string{"GET", "k"}},
			{Args: []string{"SELECT", "1"}},
			{Args: []string{"GET", "k"}},
		}},
	}
}

func TestDifferentialVariants(t *testing.T) {
	hayakvAddr, stop := startHayakv(t)
	defer stop()
	redisAddr, stopR := startRedis8(t)
	defer stopR()
	for _, sc := range variantCorpus() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			assertReplyEqual(t, sc, runScenario(t, hayakvAddr, sc), runScenario(t, redisAddr, sc))
		})
	}
}
