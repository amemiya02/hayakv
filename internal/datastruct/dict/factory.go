package dict

import "sync"

const (
	EngineShardMap = "shardmap"
	EngineRedisDB  = "redisdb"
)

var (
	engine   = EngineShardMap
	engineMu sync.RWMutex
	dictSize = 1 << 16 // default: 65536, matches dataDictSize
)

// SetEngine selects which dict implementation MakeDict will create.
func SetEngine(name string) {
	engineMu.Lock()
	defer engineMu.Unlock()
	engine = name
}

// GetEngine returns the current engine name.
func GetEngine() string {
	engineMu.RLock()
	defer engineMu.RUnlock()
	return engine
}

// SetDictSize sets the initial size hint for MakeDict.
func SetDictSize(n int) {
	engineMu.Lock()
	defer engineMu.Unlock()
	dictSize = n
}

// MakeDict creates a dict using the currently configured engine.
// For "shardmap" it returns a ConcurrentDict; for "redisdb" it returns a RedisDict.
// The DB struct holds a dict.Dict interface, so either works.
func MakeDict() Dict {
	engineMu.RLock()
	eng := engine
	size := dictSize
	engineMu.RUnlock()
	if eng == EngineRedisDB {
		return MakeRedis(size)
	}
	return MakeConcurrent(size)
}

// MakeRedisDict creates a RedisDict (single dict with incremental rehash).
// Use this when you need a Dict interface implementation without sharding.
func MakeRedisDict(size int) *RedisDict {
	return MakeRedis(size)
}
