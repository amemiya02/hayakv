package database

import (
	"math/rand"
	"strings"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// Redis approximate-LRU/LFU constants.
const (
	lruBits      = 24
	lruClockMax  = (1 << lruBits) - 1 // 24-bit clock, like robj.lru
	lruClockRes  = 1000               // clock resolution in ms (Redis LRU_CLOCK_RESOLUTION)
	lfuLogFactor = 10                 // Redis lfu-log-factor default
	lfuInitVal   = 5                  // Redis LFU_INIT_VAL: new keys start at 5
)

// isDenyOOM reports whether a command carries the Redis "denyoom" flag (write
// commands that allocate). Reads the attached extra.signs.
func isDenyOOM(cmd *command) bool {
	if cmd == nil || cmd.extra == nil {
		return false
	}
	for _, s := range cmd.extra.signs {
		if s == redisFlagDenyOOM {
			return true
		}
	}
	return false
}

// oomErrReply is the exact Redis OOM error returned when eviction can't free
// enough memory for a denyoom command.
func oomErrReply() redis.Reply {
	return protocol.MakeErrReply("OOM command not allowed when used memory > 'maxmemory'.")
}

// evictionEnabledOrLimited reports whether a maxmemory limit is configured at
// all (any positive value). Under noeviction this still gates denyoom commands
// so they can be rejected with -OOM.
func evictionEnabledOrLimited() bool {
	return config.Properties.Maxmemory > 0
}

// lruClock returns the current coarse clock in lruClockRes-ms units, masked to
// 24 bits. Mirrors Redis getLRUClock(): (mstime/RESOLUTION) & LRU_CLOCK_MAX.
func lruClock() uint32 {
	ms := time.Now().UnixMilli()
	return uint32(ms/lruClockRes) & lruClockMax
}

// lruIdleFor returns how many clock units have elapsed since access, handling
// 24-bit wraparound exactly like Redis estimateObjectIdleTime().
func lruIdleFor(now, access uint32) uint32 {
	if now >= access {
		return now - access
	}
	return (lruClockMax - access) + now + 1
}

// lfuLogIncr probabilistically increments an 8-bit LFU counter. Higher counters
// grow more slowly: P(incr) = 1/((counter-LFU_INIT_VAL)*lfu_log_factor + 1).
// Mirrors Redis LFULogIncr().
func lfuLogIncr(counter uint8) uint8 {
	if counter == 255 {
		return 255
	}
	r := rand.Float64()
	baseval := float64(counter) - lfuInitVal
	if baseval < 0 {
		baseval = 0
	}
	p := 1.0 / (baseval*lfuLogFactor + 1)
	if r < p {
		return counter + 1
	}
	return counter
}

// lruMeta is the side-tabled access metadata for one key. We do not embed this
// in DataEntity/Robj because hayakv stores raw Go values (not a universal robj
// envelope), so a parallel dict keyed by key keeps the value layer untouched.
//
// Field `data` holds EITHER a 24-bit LRU clock (LRU policies) OR an 8-bit LFU
// counter packed in the low bits plus a 16-bit decay minute in the high bits
// (LFU policies), selected by the active maxmemory-policy. We keep both layouts
// in one uint32 exactly like Redis robj.lru.
type lruMeta struct {
	data uint32
}

// newLRUMeta builds metadata for a freshly accessed/created key under policy.
func newLRUMeta(lfu bool) lruMeta {
	if lfu {
		// high 16 bits = current minute, low 8 bits = LFU_INIT_VAL
		return lruMeta{data: (lfuNowMinutes() << 8) | uint32(lfuInitVal)}
	}
	return lruMeta{data: lruClock()}
}

func (m lruMeta) lruValue() uint32   { return m.data & lruClockMax }
func (m lruMeta) lfuCounter() uint8  { return uint8(m.data & 0xff) }
func (m lruMeta) lfuMinutes() uint32 { return (m.data >> 8) & 0xffff }

func packLFU(minutes uint32, counter uint8) uint32 {
	return ((minutes & 0xffff) << 8) | uint32(counter)
}

// lfuNowMinutes returns the current UNIX time in minutes truncated to 16 bits,
// matching Redis LFUGetTimeInMinutes().
func lfuNowMinutes() uint32 {
	return uint32(time.Now().Unix()/60) & 0xffff
}

// touchLRU records access to key under the active policy. Called from GetEntity
// (read) and PutEntity (write). For LFU it bumps the probabilistic counter (with
// time-decay applied first); for LRU it stamps the coarse clock.
func (db *DB) touchLRU(key string) {
	if usingLFU() {
		raw, ok := db.lruMap.Get(key)
		var counter uint8 = lfuInitVal
		if ok {
			m := raw.(lruMeta)
			counter = lfuDecay(m)
		}
		counter = lfuLogIncr(counter)
		db.lruMap.Put(key, lruMeta{data: packLFU(lfuNowMinutes(), counter)})
		return
	}
	db.lruMap.Put(key, lruMeta{data: lruClock()})
}

// lfuDecay halves the counter once per N decay-minutes elapsed since last touch
// (Redis lfu-decay-time default 1 minute), floored at 0. Returns decayed value.
func lfuDecay(m lruMeta) uint8 {
	const decayMinutes = 1 // lfu-decay-time default
	elapsed := lfuMinutesDiff(lfuNowMinutes(), m.lfuMinutes())
	counter := m.lfuCounter()
	if decayMinutes > 0 {
		periods := elapsed / decayMinutes
		if uint32(counter) <= periods {
			return 0
		}
		return counter - uint8(periods)
	}
	return counter
}

func lfuMinutesDiff(now, then uint32) uint32 {
	if now >= then {
		return now - then
	}
	return (0xffff - then) + now + 1
}

// usingLFU reports whether the active maxmemory-policy is an LFU variant.
func usingLFU() bool {
	switch config.Properties.MaxmemoryPolicy {
	case "allkeys-lfu", "volatile-lfu":
		return true
	}
	return false
}

// maxLoops on the eviction loop so we never spin forever if the estimator and
// maxmemory disagree about reachable headroom.
const maxEvictLoops = 100000

// evictionEnabled reports whether maxmemory is set AND policy is not noeviction.
func evictionEnabled() bool {
	return config.Properties.Maxmemory > 0 &&
		config.Properties.MaxmemoryPolicy != "noeviction" &&
		config.Properties.MaxmemoryPolicy != ""
}

// freeMemoryIfNeeded evicts keys until usedMemory <= maxmemory, per the active
// policy. Returns true if memory is within budget afterwards (or no limit set),
// false if it could not free enough (noeviction over-limit, or nothing evictable).
//
// Runs on the command/cron goroutine, taking per-key write locks for each
// eviction (same discipline as activeExpireCycle), so it is correct under the
// goroutine+redisdb global-lock triangle and inline under eventloop.
//
// We compute currentMemory once and decrement it incrementally as keys are
// evicted, avoiding repeated full traversals (usedMemory is O(n) in the total
// key count).  evictOneKeyWithSize returns with key locks already released, so
// calling usedMemory() after it would not deadlock — but it would be wasteful.
func (server *Server) freeMemoryIfNeeded() bool {
	limit := config.Properties.Maxmemory
	if limit <= 0 {
		return true // unlimited
	}
	if config.Properties.MaxmemoryPolicy == "noeviction" || config.Properties.MaxmemoryPolicy == "" {
		return server.usedMemory() <= limit
	}
	currentMemory := server.usedMemory()
	if currentMemory <= limit {
		return true
	}
	for loops := 0; loops < maxEvictLoops; loops++ {
		if currentMemory <= limit {
			return true
		}
		freed, evicted := server.evictOneKeyWithSize()
		if !evicted {
			return false // nothing left to evict
		}
		currentMemory -= freed
	}
	return currentMemory <= limit
}

// evictOneKey selects and removes a single best-candidate key across all dbs.
// Returns false if no eligible key exists under the policy.
func (server *Server) evictOneKey() bool {
	_, ok := server.evictOneKeyWithSize()
	return ok
}

// evictOneKeyWithSize selects and removes a single best-candidate key across
// all dbs, returning the estimated bytes freed and true, or (0, false) if no
// eligible key exists.  The size estimate is taken under the key write lock
// (before Remove) so the entity is still live.
func (server *Server) evictOneKeyWithSize() (int64, bool) {
	policy := config.Properties.MaxmemoryPolicy
	volatile := strings.HasPrefix(policy, "volatile-")
	samples := config.Properties.MaxmemorySamples
	if samples <= 0 {
		samples = 5
	}
	for i := range server.dbSet {
		db := server.mustSelectDB(i)
		src := db.data
		if volatile {
			src = db.ttlMap
		}
		if src.Len() == 0 {
			continue
		}
		sample := src.RandomDistinctKeys(samples)
		if len(sample) == 0 {
			continue
		}
		best := db.evictionCandidates(sample)
		if best == "" {
			continue
		}
		db.RWLocks([]string{best}, nil)
		// Estimate size before removing.
		var freed int64
		if raw, ok := db.data.GetWithLock(best); ok {
			entity, _ := raw.(*database.DataEntity)
			freed = estimateEntitySize(best, entity)
		}
		db.Remove(best)
		db.addAof(toExpireDelAof(best))
		db.RWUnLocks([]string{best}, nil)
		return freed, true
	}
	return 0, false
}

// evictionCandidates picks the single best eviction target from sample under the
// active policy: lru->largest idle, lfu->smallest counter, ttl->soonest expiry,
// random->first. Returns "" if sample is empty.
func (db *DB) evictionCandidates(sample []string) string {
	if len(sample) == 0 {
		return ""
	}
	policy := config.Properties.MaxmemoryPolicy
	switch {
	case strings.HasSuffix(policy, "-random"):
		return sample[0]
	case strings.HasSuffix(policy, "-ttl"):
		// evict the key with the nearest expiration (smallest expireAt)
		best := ""
		var bestAt time.Time
		for _, k := range sample {
			raw, ok := db.ttlMap.Get(k)
			if !ok {
				continue
			}
			at, _ := raw.(time.Time)
			if best == "" || at.Before(bestAt) {
				best, bestAt = k, at
			}
		}
		if best == "" {
			return sample[0]
		}
		return best
	case strings.HasSuffix(policy, "-lfu"):
		best := ""
		var bestCounter uint8 = 255
		for _, k := range sample {
			c := db.keyLFUCounter(k)
			if best == "" || c < bestCounter {
				best, bestCounter = k, c
			}
		}
		return best
	default: // -lru
		best := ""
		var bestIdle uint32
		now := lruClock()
		for _, k := range sample {
			idle := db.keyIdle(now, k)
			if best == "" || idle > bestIdle {
				best, bestIdle = k, idle
			}
		}
		return best
	}
}

// keyIdle returns the LRU idle (in clock units) for key; keys with no recorded
// access are treated as maximally idle (evict first).
func (db *DB) keyIdle(now uint32, key string) uint32 {
	raw, ok := db.lruMap.Get(key)
	if !ok {
		return lruClockMax
	}
	m := raw.(lruMeta)
	return lruIdleFor(now, m.lruValue())
}

// keyLFUCounter returns the (decayed) LFU counter for key; keys with no record
// are treated as counter 0 (evict first).
func (db *DB) keyLFUCounter(key string) uint8 {
	raw, ok := db.lruMap.Get(key)
	if !ok {
		return 0
	}
	return lfuDecay(raw.(lruMeta))
}
