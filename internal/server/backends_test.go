package server

import (
	"testing"

	"github.com/amemiya02/hayakv/config"
)

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
