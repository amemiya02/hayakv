package server

import (
	"testing"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2"
)

// stubEngine satisfies iface.StorageEngine for tests.
type stubEngine struct{}

func (stubEngine) Exec(_ iredis.Connection, _ iface.CmdLine) iredis.Reply { return nil }
func (stubEngine) AfterClientClose(_ iredis.Connection)                   {}
func (stubEngine) Close()                                                 {}

func TestDefaultBackendNames(t *testing.T) {
	cfg := &config.ServerProperties{}
	NormalizeBackends(cfg)

	if cfg.NetBackend != "goroutine" {
		t.Fatalf("net backend = %q, want goroutine", cfg.NetBackend)
	}
	if cfg.EngineBackend != "shardmap" {
		t.Fatalf("engine backend = %q, want shardmap", cfg.EngineBackend)
	}
	if cfg.ProtoMax != "resp2" {
		t.Fatalf("proto-max = %q, want resp2", cfg.ProtoMax)
	}
}

func TestM0BackendSelection(t *testing.T) {
	cfg := &config.ServerProperties{
		NetBackend:    "goroutine",
		EngineBackend: "shardmap",
		ProtoMax:      "resp2",
	}

	engine, err := NewStorageEngine(cfg)
	if err != nil {
		t.Fatalf("NewStorageEngine returned error: %v", err)
	}
	if engine == nil {
		t.Fatal("NewStorageEngine returned nil")
	}

	codec, err := NewProtocolCodec(cfg)
	if err != nil {
		t.Fatalf("NewProtocolCodec returned error: %v", err)
	}
	if codec == nil {
		t.Fatal("NewProtocolCodec returned nil")
	}
}

func TestNewProtocolCodecRESP3(t *testing.T) {
	codec, err := NewProtocolCodec(&config.ServerProperties{ProtoMax: "resp3"})
	if err != nil {
		t.Fatalf("resp3 codec should be available in M1: %v", err)
	}
	if codec == nil {
		t.Fatal("nil codec")
	}
}

func TestEventloopBackend(t *testing.T) {
	cfg := &config.ServerProperties{NetBackend: "eventloop", EngineBackend: "shardmap", ProtoMax: "resp2"}
	NormalizeBackends(cfg)
	engine, err := NewStorageEngine(cfg)
	if err != nil {
		t.Fatalf("NewStorageEngine: %v", err)
	}
	srv, err := NewNetServerWithEngine(cfg, engine)
	if err != nil {
		t.Fatalf("NewNetServerWithEngine(eventloop): %v", err)
	}
	if srv == nil {
		t.Fatal("NewNetServerWithEngine returned nil")
	}
}

func TestRedisDBBackend(t *testing.T) {
	cfg := &config.ServerProperties{
		NetBackend:    "goroutine",
		EngineBackend: "redisdb",
		ProtoMax:      "resp2",
	}
	engine, err := NewStorageEngine(cfg)
	if err != nil {
		t.Fatalf("NewStorageEngine(redisdb) returned error: %v", err)
	}
	if engine == nil {
		t.Fatal("NewStorageEngine(redisdb) returned nil")
	}
}

func TestMaybeWrapClusterRedis(t *testing.T) {
	cfg := &config.ServerProperties{ClusterEnable: true, ClusterMode: "redis",
		Bind: "127.0.0.1", Port: 7000, Dir: t.TempDir(), ClusterConfigFile: "nodes.conf"}
	wrapped, err := MaybeWrapCluster(cfg, stubEngine{})
	if err != nil {
		t.Fatalf("MaybeWrapCluster: %v", err)
	}
	// CLUSTER MYID must be answered by the wrapper (40 hex), not the stub.
	r := wrapped.Exec(nil, [][]byte{[]byte("CLUSTER"), []byte("MYID")})
	out := resp2.Codec{}.Encode(r, iredis.RESP2)
	if len(out) == 0 || out[0] != '$' {
		t.Fatalf("wrapped CLUSTER MYID = %q, want bulk id", out)
	}
}

func TestMaybeWrapClusterRaftPassThrough(t *testing.T) {
	cfg := &config.ServerProperties{ClusterEnable: true, ClusterMode: "raft"}
	wrapped, err := MaybeWrapCluster(cfg, stubEngine{})
	if err != nil {
		t.Fatalf("MaybeWrapCluster: %v", err)
	}
	if _, isCluster := wrapped.(interface{ Commands() interface{} }); isCluster {
		t.Fatal("raft mode must NOT wrap with the redis cluster engine")
	}
}

func TestMaybeWrapClusterDisabled(t *testing.T) {
	cfg := &config.ServerProperties{ClusterEnable: false}
	wrapped, _ := MaybeWrapCluster(cfg, stubEngine{})
	if _, ok := wrapped.(stubEngine); !ok {
		t.Fatal("cluster disabled must return the engine unchanged")
	}
}
