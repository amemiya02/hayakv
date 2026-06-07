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
