package diff

import (
	"strings"
	"testing"

	database "github.com/amemiya02/hayakv/internal/command"
)

var m0DiffExclusions = map[string]string{
	"append":            "covered by string set/get corpus; dedicated append scenario deferred to M2 string-expansion",
	"auth":              "requires auth config matrix, covered after M0 baseline connectivity",
	"bgsave":            "persistence control command is M6 scope",
	"blpop":             "blocking command; requires eventloop/timeout semantics, deferred to M3 blocking corpus",
	"brpop":             "blocking command; requires eventloop/timeout semantics, deferred to M3 blocking corpus",
	"bitcount":          "bitfield commands deferred to M2 bit-command expansion",
	"bitpos":            "bitfield commands deferred to M2 bit-command expansion",
	"command":           "server metadata differs from Redis 8 until command table is redesigned",
	"copyfrom":          "godis internal cluster helper command, not public Redis core command",
	"decr":              "covered by string numeric corpus (incr/decrby); decr variant deferred to M2",
	"copyto":            "godis internal cluster helper command, not public Redis core command",
	"discard":           "transaction command; transaction corpus deferred to M3",
	"dumpkey":           "godis internal cluster helper command, not public Redis core command",
	"exec":              "transaction command; transaction corpus deferred to M3",
	"existin":           "godis internal cluster helper command, not public Redis core command",
	"exists":            "covered by keys scenario indirectly; dedicated exists scenario deferred to M2",
	"expireat":          "covered by expire/persist/ttl corpus; expireat variant deferred to M2",
	"expiretime":        "covered by expire/persist/ttl corpus; expiretime variant deferred to M2",
	"flushall":          "covered by harness setup isolation rather than corpus command",
	"flushdb":           "covered by harness setup isolation rather than corpus command",
	"geoadd":            "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"geodist":           "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"geohash":           "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"geopos":            "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"georadius":         "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"georadiusbymember": "geo commands deferred; geo formatting needs Redis 8 precision audit after baseline harness lands",
	"getbit":            "bitfield commands deferred to M2 bit-command expansion",
	"getdel":            "covered by string set/get/del corpus; getdel variant deferred to M2",
	"getex":             "covered by string set/get corpus; getex variant deferred to M2",
	"getrange":          "covered by string set/get corpus; getrange variant deferred to M2",
	"getset":            "covered by string set/get corpus; getset variant deferred to M2",
	"getver":            "godis internal version command, not public Redis core command",
	"hexists":           "covered by hash corpus; hexists variant deferred to M2",
	"hgetall":           "covered by hash corpus; hgetall variant deferred to M2 hash-expansion",
	"hincrby":           "covered by hash corpus; hincrby variant deferred to M2",
	"hincrbyfloat":      "covered by hash corpus; hincrbyfloat variant deferred to M2",
	"hkeys":             "covered by hash corpus; hkeys variant deferred to M2 hash-expansion",
	"hmget":             "covered by hash corpus; hmget variant deferred to M2",
	"hmset":             "covered by hash corpus; hmset variant deferred to M2",
	"hrandfield":        "covered by hash corpus; hrandfield variant deferred to M2",
	"hscan":             "cursor ordering differs until Redis dict scan is implemented in M2",
	"hsetnx":            "covered by hash corpus; hsetnx variant deferred to M2",
	"hstrlen":           "covered by hash corpus; hstrlen variant deferred to M2",
	"hvals":             "covered by hash corpus; hvals variant deferred to M2 hash-expansion",
	"incrby":            "covered by string numeric corpus (incr/decrby); incrby variant deferred to M2",
	"incrbyfloat":       "covered by string numeric corpus; incrbyfloat variant deferred to M2",
	"info":              "contains server-specific fields that need normalization",
	"keys":              "unordered output is unsuitable for byte-for-byte corpus without normalization",
	"lindex":            "covered by list corpus; lindex variant deferred to M2",
	"linsert":           "covered by list corpus; linsert variant deferred to M2",
	"lpushx":            "covered by list corpus; lpushx variant deferred to M2",
	"lrem":              "covered by list corpus; lrem variant deferred to M2",
	"lset":              "covered by list corpus; lset variant deferred to M2",
	"ltrim":             "covered by list corpus; ltrim variant deferred to M2",
	"mget":              "covered by string set/get corpus; mget variant deferred to M2",
	"mset":              "covered by string set/get corpus; mset variant deferred to M2",
	"msetnx":            "covered by string set/get corpus; msetnx variant deferred to M2",
	"multi":             "transaction command; transaction corpus deferred to M3",
	"object":            "object introspection command; covered by M3 object encoding corpus",
	"pexpire":           "covered by expire/persist/ttl corpus; pexpire variant deferred to M2",
	"pexpireat":         "covered by expire/persist/ttl corpus; pexpireat variant deferred to M2",
	"pexpiretime":       "covered by expire/persist/ttl corpus; pexpiretime variant deferred to M2",
	"psetex":            "covered by string set/get corpus; psetex variant deferred to M2",
	"pttl":              "covered by expire/persist/ttl corpus; pttl variant deferred to M2",
	"publish":           "pub/sub requires multiple connections; add after single-connection harness",
	"randomkey":         "nondeterministic by design",
	"rename":            "covered by string set/get corpus; rename variant deferred to M2",
	"renamefrom":        "godis internal rename helper, not public Redis core command",
	"renamenx":          "covered by string set/get corpus; renamenx variant deferred to M2",
	"renamenxto":        "godis internal rename helper, not public Redis core command",
	"renameto":          "godis internal rename helper, not public Redis core command",
	"replconf":          "replication is M7 scope",
	"rpop":              "covered by list corpus; rpop variant deferred to M2",
	"rpoplpush":         "covered by list corpus; rpoplpush variant deferred to M2",
	"rpushx":            "covered by list corpus; rpushx variant deferred to M2",
	"save":              "persistence control command is M6 scope",
	"scan":              "cursor ordering differs until Redis dict scan is implemented in M2",
	"sdiff":             "covered by set corpus; sdiff variant deferred to M2 set-expansion",
	"sdiffstore":        "covered by set corpus; sdiffstore variant deferred to M2 set-expansion",
	"select":            "covered indirectly by future multi-db corpus",
	"setbit":            "bitfield commands deferred to M2 bit-command expansion",
	"setex":             "covered by string set/get corpus; setex variant deferred to M2",
	"setnx":             "covered by string set/get corpus; setnx variant deferred to M2",
	"setrange":          "covered by string set/get corpus; setrange variant deferred to M2",
	"sinter":            "covered by set corpus; sinter variant deferred to M2 set-expansion",
	"sinterstore":       "covered by set corpus; sinterstore variant deferred to M2 set-expansion",
	"smembers":          "covered by set corpus; smembers variant deferred to M2",
	"spop":              "covered by set corpus; spop variant deferred to M2",
	"srandmember":       "covered by set corpus; srandmember variant deferred to M2",
	"sscan":             "cursor ordering differs until Redis dict scan is implemented in M2",
	"slaveof":           "replication is M7 scope",
	"strlen":            "covered by string set/get corpus; strlen variant deferred to M2",
	"subscribe":         "pub/sub requires multiple connections; add after single-connection harness",
	"sunion":            "covered by set corpus; sunion variant deferred to M2 set-expansion",
	"sunionstore":       "covered by set corpus; sunionstore variant deferred to M2 set-expansion",
	"type":              "covered by keys/ttl corpus indirectly; dedicated type scenario deferred to M2",
	"watch":             "transaction command; transaction corpus deferred to M3",
	"zcard":             "covered by zset corpus; zcard variant deferred to M2",
	"zcount":            "covered by zset corpus; zcount variant deferred to M2",
	"zincrby":           "covered by zset corpus; zincrby variant deferred to M2",
	"zlexcount":         "covered by zset corpus; zlexcount variant deferred to M2",
	"zpopmin":           "covered by zset corpus; zpopmin variant deferred to M2",
	"zrangebylex":       "covered by zset corpus; zrangebylex variant deferred to M2",
	"zrangebyscore":     "covered by zset corpus; zrangebyscore variant deferred to M2",
	"zrank":             "covered by zset corpus; zrank variant deferred to M2",
	"zremrangebylex":    "covered by zset corpus; zremrangebylex variant deferred to M2",
	"zremrangebyrank":   "covered by zset corpus; zremrangebyrank variant deferred to M2",
	"zremrangebyscore":  "covered by zset corpus; zremrangebyscore variant deferred to M2",
	"zrevrange":         "covered by zset corpus; zrevrange variant deferred to M2",
	"zrevrangebylex":    "covered by zset corpus; zrevrangebylex variant deferred to M2",
	"zrevrangebyscore":  "covered by zset corpus; zrevrangebyscore variant deferred to M2",
	"zrevrank":          "covered by zset corpus; zrevrank variant deferred to M2",
	"zscan":             "cursor ordering differs until Redis dict scan is implemented in M2",
}

func TestM0CorpusMentionsOrExcludesEveryRegisteredCommand(t *testing.T) {
	covered := map[string]bool{}
	for _, scenario := range m0Corpus() {
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
		if reason := m0DiffExclusions[name]; reason == "" {
			t.Fatalf("registered command %q has no M0 diff scenario or exclusion reason", name)
		}
	}
}
