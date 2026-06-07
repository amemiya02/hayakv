package database

import (
	"math/rand"
	"time"

	"github.com/amemiya02/hayakv/config"
)

// Redis approximate-LRU/LFU constants.
const (
	lruBits      = 24
	lruClockMax  = (1 << lruBits) - 1 // 24-bit clock, like robj.lru
	lruClockRes  = 1000               // clock resolution in ms (Redis LRU_CLOCK_RESOLUTION)
	lfuLogFactor = 10                 // Redis lfu-log-factor default
	lfuInitVal   = 5                  // Redis LFU_INIT_VAL: new keys start at 5
)

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

func (m lruMeta) lruValue() uint32 { return m.data & lruClockMax }
func (m lruMeta) lfuCounter() uint8 { return uint8(m.data & 0xff) }
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
