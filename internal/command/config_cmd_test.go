package database

import (
	"os"
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestConfigGetSetMaxmemoryPolicy(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "noeviction"
	defer func() { config.Properties.MaxmemoryPolicy = "noeviction" }()

	set := execConfig(nil, [][]byte{[]byte("SET"), []byte("maxmemory-policy"), []byte("allkeys-lru")})
	if _, ok := set.(*protocol.OkReply); !ok {
		t.Fatalf("CONFIG SET should return OK, got %T", set)
	}
	if config.Properties.MaxmemoryPolicy != "allkeys-lru" {
		t.Fatalf("policy not applied: %q", config.Properties.MaxmemoryPolicy)
	}
	get := execConfig(nil, [][]byte{[]byte("GET"), []byte("maxmemory-policy")})
	mb, ok := get.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("CONFIG GET should return array, got %T", get)
	}
	body := string(mb.ToBytes())
	if !strings.Contains(body, "maxmemory-policy") || !strings.Contains(body, "allkeys-lru") {
		t.Fatalf("CONFIG GET body = %q", body)
	}
}

func TestConfigSetMaxmemoryByteSuffix(t *testing.T) {
	execConfig(nil, [][]byte{[]byte("SET"), []byte("maxmemory"), []byte("10mb")})
	if config.Properties.Maxmemory != 10*1024*1024 {
		t.Fatalf("CONFIG SET maxmemory 10mb = %d", config.Properties.Maxmemory)
	}
	execConfig(nil, [][]byte{[]byte("SET"), []byte("maxmemory"), []byte("0")})
}

func TestConfigGetReturnsSuiteParams(t *testing.T) {
	for _, p := range []string{"maxmemory", "maxmemory-policy", "appendonly",
		"list-max-listpack-size", "hash-max-listpack-entries", "set-max-intset-entries", "zset-max-listpack-entries"} {
		if len(configGet(p)) == 0 {
			t.Fatalf("CONFIG GET %s returned nothing", p)
		}
	}
}

func TestConfigGetWildcard(t *testing.T) {
	// "maxmemory*" should match maxmemory, maxmemory-policy, maxmemory-samples
	result := configGet("maxmemory*")
	if len(result) < 6 { // at least 3 key-value pairs = 6 entries
		t.Fatalf("CONFIG GET maxmemory* returned too few results: %d entries", len(result))
	}
	// "*" should match everything
	all := configGet("*")
	if len(all) < 10 { // at least 5 known params
		t.Fatalf("CONFIG GET * returned too few results: %d entries", len(all))
	}
}

func TestConfigRewrite(t *testing.T) {
	dir := t.TempDir()
	confPath := dir + "/redis.conf"
	os.WriteFile(confPath, []byte("port 6399\nmaxmemory 0\n"), 0644)
	config.SetupConfig(confPath)
	config.Properties.Maxmemory = 100 * 1024 * 1024

	c := connection.NewFakeConn()
	r := testServer.Exec(c, utils.ToCmdLine("CONFIG", "REWRITE"))
	asserts.AssertStatusReply(t, r, "OK")

	out, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read rewritten config: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "maxmemory") {
		t.Fatalf("rewritten config missing maxmemory: %s", content)
	}
	if !strings.Contains(content, "port") {
		t.Fatalf("rewritten config missing port: %s", content)
	}
}

func TestConfigResetstat(t *testing.T) {
	c := connection.NewFakeConn()
	testServer.Exec(c, utils.ToCmdLine("SET", "k", "v"))
	r := testServer.Exec(c, utils.ToCmdLine("CONFIG", "RESETSTAT"))
	asserts.AssertStatusReply(t, r, "OK")
}

func TestConfigGetAdditionalParams(t *testing.T) {
	for _, p := range []string{"latency-monitor-threshold", "slowlog-log-slower-than",
		"slowlog-max-len", "port", "bind", "dir", "dbfilename"} {
		if len(configGet(p)) == 0 {
			t.Fatalf("CONFIG GET %s returned nothing", p)
		}
	}
}
