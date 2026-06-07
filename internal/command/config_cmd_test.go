package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/config"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestConfigGetSetMaxmemoryPolicy(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "noeviction"
	defer func() { config.Properties.MaxmemoryPolicy = "noeviction" }()
	var _ iredis.Connection = connection.NewConn(nil)

	set := execConfig([][]byte{[]byte("SET"), []byte("maxmemory-policy"), []byte("allkeys-lru")})
	if _, ok := set.(*protocol.OkReply); !ok {
		t.Fatalf("CONFIG SET should return OK, got %T", set)
	}
	if config.Properties.MaxmemoryPolicy != "allkeys-lru" {
		t.Fatalf("policy not applied: %q", config.Properties.MaxmemoryPolicy)
	}
	get := execConfig([][]byte{[]byte("GET"), []byte("maxmemory-policy")})
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
	execConfig([][]byte{[]byte("SET"), []byte("maxmemory"), []byte("10mb")})
	if config.Properties.Maxmemory != 10*1024*1024 {
		t.Fatalf("CONFIG SET maxmemory 10mb = %d", config.Properties.Maxmemory)
	}
	execConfig([][]byte{[]byte("SET"), []byte("maxmemory"), []byte("0")})
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
