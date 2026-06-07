package diff

import (
	"strings"
	"testing"

	database "github.com/amemiya02/hayakv/internal/command"
)

var diffExclusions = map[string]string{
	"append":            "covered by string set/get corpus; dedicated append scenario not yet added",
	"auth":              "requires an auth config matrix; not yet in the corpus",
	"bgsave":            "persistence control command; covered by persistence and integration tests",
	"blpop":             "blocking command; timeout semantics are not byte-diffable in this harness",
	"brpop":             "blocking command; timeout semantics are not byte-diffable in this harness",
	"bitcount":          "dedicated bit-command scenarios not yet added",
	"bitpos":            "dedicated bit-command scenarios not yet added",
	"command":           "server metadata differs from Redis 8 until command table is redesigned",
	"copyfrom":          "godis internal cluster helper command, not public Redis core command",
	"decr":              "covered by string numeric corpus (incr/decrby); decr variant not yet added",
	"copyto":            "godis internal cluster helper command, not public Redis core command",
	"discard":           "transaction command; transaction corpus not yet added",
	"dumpkey":           "godis internal cluster helper command, not public Redis core command",
	"exec":              "transaction command; transaction corpus not yet added",
	"existin":           "godis internal cluster helper command, not public Redis core command",
	"exists":            "covered by keys scenario indirectly; dedicated exists scenario not yet added",
	"expireat":          "covered by expire/persist/ttl corpus; expireat variant not yet added",
	"expiretime":        "covered by expire/persist/ttl corpus; expiretime variant not yet added",
	"flushall":          "covered by harness setup isolation rather than corpus command",
	"flushdb":           "covered by harness setup isolation rather than corpus command",
	"geoadd":            "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"geodist":           "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"geohash":           "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"geopos":            "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"georadius":         "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"georadiusbymember": "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"getbit":            "dedicated bit-command scenarios not yet added",
	"getdel":            "covered by string set/get/del corpus; getdel variant not yet added",
	"getex":             "covered by string set/get corpus; getex variant not yet added",
	"getrange":          "covered by string set/get corpus; getrange variant not yet added",
	"getset":            "covered by string set/get corpus; getset variant not yet added",
	"getver":            "godis internal version command, not public Redis core command",
	"hexists":           "covered by hash corpus; hexists variant not yet added",
	"hgetall":           "covered by hash corpus; hgetall variant not yet added",
	"hincrby":           "covered by hash corpus; hincrby variant not yet added",
	"hincrbyfloat":      "covered by hash corpus; hincrbyfloat variant not yet added",
	"hkeys":             "covered by hash corpus; hkeys variant not yet added",
	"hmget":             "covered by hash corpus; hmget variant not yet added",
	"hmset":             "covered by hash corpus; hmset variant not yet added",
	"hrandfield":        "covered by hash corpus; hrandfield variant not yet added",
	"hscan":             "cursor ordering differs; SCAN is exercised by the redisdb corpus instead",
	"hsetnx":            "covered by hash corpus; hsetnx variant not yet added",
	"hstrlen":           "covered by hash corpus; hstrlen variant not yet added",
	"hvals":             "covered by hash corpus; hvals variant not yet added",
	"incrby":            "covered by string numeric corpus (incr/decrby); incrby variant not yet added",
	"incrbyfloat":       "covered by string numeric corpus; incrbyfloat variant not yet added",
	"info":              "contains server-specific fields that need normalization",
	"keys":              "unordered output is unsuitable for byte-for-byte corpus without normalization",
	"lindex":            "covered by list corpus; lindex variant not yet added",
	"linsert":           "covered by list corpus; linsert variant not yet added",
	"lpushx":            "covered by list corpus; lpushx variant not yet added",
	"lrem":              "covered by list corpus; lrem variant not yet added",
	"lset":              "covered by list corpus; lset variant not yet added",
	"ltrim":             "covered by list corpus; ltrim variant not yet added",
	"memory":            "MEMORY USAGE reports allocator-specific byte counts that differ from real Redis by design",
	"mget":              "covered by string set/get corpus; mget variant not yet added",
	"mset":              "covered by string set/get corpus; mset variant not yet added",
	"msetnx":            "covered by string set/get corpus; msetnx variant not yet added",
	"multi":             "transaction command; transaction corpus not yet added",
	"object":            "object introspection command; covered by the object-encoding tests",
	"pexpire":           "covered by expire/persist/ttl corpus; pexpire variant not yet added",
	"pexpireat":         "covered by expire/persist/ttl corpus; pexpireat variant not yet added",
	"pexpiretime":       "covered by expire/persist/ttl corpus; pexpiretime variant not yet added",
	"psetex":            "covered by string set/get corpus; psetex variant not yet added",
	"pttl":              "covered by expire/persist/ttl corpus; pttl variant not yet added",
	"publish":           "pub/sub requires multiple connections; add after single-connection harness",
	"randomkey":         "nondeterministic by design",
	"rename":            "covered by string set/get corpus; rename variant not yet added",
	"renamefrom":        "godis internal rename helper, not public Redis core command",
	"renamenx":          "covered by string set/get corpus; renamenx variant not yet added",
	"renamenxto":        "godis internal rename helper, not public Redis core command",
	"renameto":          "godis internal rename helper, not public Redis core command",
	"replconf":          "replication command; covered by integration replication tests",
	"rpop":              "covered by list corpus; rpop variant not yet added",
	"rpoplpush":         "covered by list corpus; rpoplpush variant not yet added",
	"rpushx":            "covered by list corpus; rpushx variant not yet added",
	"save":              "persistence control command; covered by persistence and integration tests",
	"scan":              "cursor ordering differs; SCAN is exercised by the redisdb corpus instead",
	"sdiff":             "covered by set corpus; sdiff variant not yet added",
	"sdiffstore":        "covered by set corpus; sdiffstore variant not yet added",
	"select":            "covered indirectly by future multi-db corpus",
	"setbit":            "dedicated bit-command scenarios not yet added",
	"setex":             "covered by string set/get corpus; setex variant not yet added",
	"setnx":             "covered by string set/get corpus; setnx variant not yet added",
	"setrange":          "covered by string set/get corpus; setrange variant not yet added",
	"sinter":            "covered by set corpus; sinter variant not yet added",
	"sinterstore":       "covered by set corpus; sinterstore variant not yet added",
	"smembers":          "covered by set corpus; smembers variant not yet added",
	"spop":              "covered by set corpus; spop variant not yet added",
	"srandmember":       "covered by set corpus; srandmember variant not yet added",
	"sscan":             "cursor ordering differs; SCAN is exercised by the redisdb corpus instead",
	"slaveof":           "replication command; covered by integration replication tests",
	"strlen":            "covered by string set/get corpus; strlen variant not yet added",
	"subscribe":         "pub/sub requires multiple connections; add after single-connection harness",
	"sunion":            "covered by set corpus; sunion variant not yet added",
	"sunionstore":       "covered by set corpus; sunionstore variant not yet added",
	"type":              "covered by keys/ttl corpus indirectly; dedicated type scenario not yet added",
	"watch":             "transaction command; transaction corpus not yet added",
	"zcard":             "covered by zset corpus; zcard variant not yet added",
	"zcount":            "covered by zset corpus; zcount variant not yet added",
	"zincrby":           "covered by zset corpus; zincrby variant not yet added",
	"zlexcount":         "covered by zset corpus; zlexcount variant not yet added",
	"zpopmin":           "covered by zset corpus; zpopmin variant not yet added",
	"zrangebylex":       "covered by zset corpus; zrangebylex variant not yet added",
	"zrangebyscore":     "covered by zset corpus; zrangebyscore variant not yet added",
	"zrank":             "covered by zset corpus; zrank variant not yet added",
	"zremrangebylex":    "covered by zset corpus; zremrangebylex variant not yet added",
	"zremrangebyrank":   "covered by zset corpus; zremrangebyrank variant not yet added",
	"zremrangebyscore":  "covered by zset corpus; zremrangebyscore variant not yet added",
	"zrevrange":         "covered by zset corpus; zrevrange variant not yet added",
	"zrevrangebylex":    "covered by zset corpus; zrevrangebylex variant not yet added",
	"zrevrangebyscore":  "covered by zset corpus; zrevrangebyscore variant not yet added",
	"zrevrank":          "covered by zset corpus; zrevrank variant not yet added",
	"zscan":             "cursor ordering differs; SCAN is exercised by the redisdb corpus instead",
}

func TestCorpusMentionsOrExcludesEveryRegisteredCommand(t *testing.T) {
	covered := map[string]bool{}
	for _, scenario := range baseCorpus() {
		for _, cmd := range scenario.Commands {
			if len(cmd.Args) > 0 {
				covered[strings.ToLower(cmd.Args[0])] = true
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
