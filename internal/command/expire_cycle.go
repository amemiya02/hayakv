package database

import (
	"time"

	"github.com/amemiya02/hayakv/config"
)

// activeExpireConfig tunes one active-expire pass (mirrors Redis
// activeExpireCycle knobs). sampleSize = keys sampled per loop;
// maxLoops bounds work per db per tick.
type activeExpireConfig struct {
	sampleSize int
	maxLoops   int
}

const (
	activeExpireKeysPerLoop     = 20 // Redis ACTIVE_EXPIRE_CYCLE_KEYS_PER_LOOP
	activeExpireAcceptableStale = 25 // percent: keep looping while >25% sampled keys were expired
)

// activeExpireCycle samples keys with a TTL and reaps the expired ones, looping
// while the expired ratio stays above activeExpireAcceptableStale or until
// maxLoops is hit. Returns the number of keys reaped. Loop-/lock-safe: it takes
// the same per-key write lock the command path uses (RWLocks), so it is correct
// under the goroutine+redisdb global-lock triangle.
func (db *DB) activeExpireCycle(cfg activeExpireConfig) int {
	if cfg.sampleSize <= 0 {
		cfg.sampleSize = activeExpireKeysPerLoop
	}
	if cfg.maxLoops <= 0 {
		cfg.maxLoops = 16
	}
	total := 0
	for loop := 0; loop < cfg.maxLoops; loop++ {
		ttlCount := db.ttlMap.Len()
		if ttlCount == 0 {
			break
		}
		sample := db.ttlMap.RandomDistinctKeys(cfg.sampleSize)
		if len(sample) == 0 {
			break
		}
		expired := 0
		for _, key := range sample {
			if db.expireIfNeeded(key) {
				expired++
				total++
			}
		}
		// Stop early if few keys are stale (Redis: <25% of the sampled set).
		if expired*100/len(sample) <= activeExpireAcceptableStale {
			break
		}
	}
	return total
}

// expireIfNeeded reaps key if its TTL is past due, under a per-key write lock.
// Returns true if the key was removed. Re-checks under the lock (check-lock-check)
// because the TTL may change while waiting.
func (db *DB) expireIfNeeded(key string) bool {
	rawTTL, ok := db.ttlMap.Get(key)
	if !ok {
		return false
	}
	expireTime, _ := rawTTL.(time.Time)
	if !time.Now().After(expireTime) {
		return false
	}
	db.RWLocks([]string{key}, nil)
	defer db.RWUnLocks([]string{key}, nil)
	rawTTL, ok = db.ttlMap.Get(key)
	if !ok {
		return false
	}
	expireTime, _ = rawTTL.(time.Time)
	if !time.Now().After(expireTime) {
		return false
	}
	db.Remove(key)
	db.addAof(toExpireDelAof(key))
	return true
}

// toExpireDelAof builds the DEL propagated to AOF/replicas when active expire
// reaps a key (Redis propagates an explicit DEL for deterministic replication).
func toExpireDelAof(key string) CmdLine {
	return CmdLine{[]byte("DEL"), []byte(key)}
}

// serverCronPeriod returns the tick interval derived from config hz.
func serverCronPeriod() time.Duration {
	hz := config.Properties.Hz
	if hz <= 0 {
		hz = 10
	}
	return time.Second / time.Duration(hz)
}
