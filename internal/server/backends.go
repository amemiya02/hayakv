package server

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amemiya02/hayakv/config"
	database "github.com/amemiya02/hayakv/internal/command"
	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/net/eventloop"
	goroutinenet "github.com/amemiya02/hayakv/internal/net/goroutine"
	"github.com/amemiya02/hayakv/internal/proto/resp2"
	"github.com/amemiya02/hayakv/internal/proto/resp3"
	"github.com/amemiya02/hayakv/internal/rediscluster"
)

const (
	NetGoroutine   = "goroutine"
	NetEventLoop   = "eventloop"
	EngineShardMap = "shardmap"
	EngineRedisDB  = "redisdb"
	ProtoRESP2     = "resp2"
	ProtoRESP3     = "resp3"
	ClusterModeRedis = "redis"
)

func NormalizeBackends(cfg *config.ServerProperties) {
	if strings.TrimSpace(cfg.NetBackend) == "" {
		cfg.NetBackend = NetGoroutine
	}
	if strings.TrimSpace(cfg.EngineBackend) == "" {
		cfg.EngineBackend = EngineShardMap
	}
	if strings.TrimSpace(cfg.ProtoMax) == "" {
		cfg.ProtoMax = ProtoRESP2
	}
	cfg.NetBackend = strings.ToLower(cfg.NetBackend)
	cfg.EngineBackend = strings.ToLower(cfg.EngineBackend)
	cfg.ProtoMax = strings.ToLower(cfg.ProtoMax)
}

func NewStorageEngine(cfg *config.ServerProperties) (iface.StorageEngine, error) {
	NormalizeBackends(cfg)
	switch cfg.EngineBackend {
	case EngineShardMap:
		dict.SetEngine("shardmap")
		return database.NewStandaloneServer(), nil
	case EngineRedisDB:
		dict.SetEngine("redisdb")
		inner := database.NewStandaloneServer()
		// For goroutine backend, wrap with a global lock since
		// redisdb uses a single non-sharded dict.
		if cfg.NetBackend == NetGoroutine {
			return NewLockedEngine(inner), nil
		}
		// For eventloop (future), locking is external.
		return inner, nil
	default:
		return nil, fmt.Errorf("unknown engine backend %q", cfg.EngineBackend)
	}
}

func NewProtocolCodec(cfg *config.ServerProperties) (iface.ProtocolCodec, error) {
	NormalizeBackends(cfg)
	switch cfg.ProtoMax {
	case ProtoRESP2:
		return resp2.Codec{}, nil
	case ProtoRESP3:
		return resp3.Codec{}, nil
	default:
		return nil, fmt.Errorf("unknown protocol backend %q", cfg.ProtoMax)
	}
}

func NewNetServer(cfg *config.ServerProperties) (iface.NetServer, error) {
	return NewNetServerWithEngine(cfg, nil)
}

// NewNetServerWithEngine creates a NetServer with an injected engine.
// For the eventloop backend, the engine is required.
func NewNetServerWithEngine(cfg *config.ServerProperties, engine iface.StorageEngine) (iface.NetServer, error) {
	NormalizeBackends(cfg)
	switch cfg.NetBackend {
	case NetGoroutine:
		return goroutinenet.NewServer(), nil
	case NetEventLoop:
		resp := iface.RESP2
		if cfg.ProtoMax == ProtoRESP3 {
			resp = iface.RESP3
		}
		return eventloop.NewServer(engine, resp), nil
	default:
		return nil, fmt.Errorf("unknown net backend %q", cfg.NetBackend)
	}
}

// MaybeWrapCluster returns engine wrapped in a Redis Cluster redirection decorator
// when cluster-enable=yes AND cluster-mode=redis; otherwise it returns engine
// unchanged (the legacy raft proxy in internal/cluster is selected elsewhere, by
// main.go, and is unaffected).
func MaybeWrapCluster(cfg *config.ServerProperties, engine iface.StorageEngine) (iface.StorageEngine, error) {
	if !cfg.ClusterEnable || strings.ToLower(cfg.ClusterMode) != ClusterModeRedis {
		return engine, nil
	}
	confName := cfg.ClusterConfigFile
	if strings.TrimSpace(confName) == "" {
		confName = "nodes.conf"
	}
	confPath := confName
	if !filepath.IsAbs(confPath) {
		dir := cfg.Dir
		if dir == "" {
			dir = "."
		}
		confPath = filepath.Join(dir, confName)
	}
	ip := cfg.AnnounceHost
	if ip == "" {
		ip = cfg.Bind
	}
	ce, err := rediscluster.NewClusterEngineFromConfig(engine, ip, cfg.Port, confPath)
	if err != nil {
		return nil, err
	}
	return ce, nil
}
