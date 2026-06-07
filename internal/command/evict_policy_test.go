package database

import (
	"testing"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/database"
)

func seedKeys(db *DB, n int, withTTL bool) {
	for i := 0; i < n; i++ {
		k := "key:" + time.Duration(i).String()
		db.PutEntity(k, &database.DataEntity{Data: []byte("payload-payload")})
		if withTTL {
			db.ttlMap.Put(k, time.Now().Add(time.Hour))
		}
	}
}

func TestPolicyNoevictionReturnsError(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "noeviction"
	config.Properties.Maxmemory = 1 // 1 byte: always over
	s := NewStandaloneServer()
	defer s.Close()
	seedKeys(s.mustSelectDB(0), 10, false)
	if ok := s.freeMemoryIfNeeded(); ok {
		t.Fatalf("noeviction must fail to free memory when over limit")
	}
}

func TestPolicyAllkeysRandomEvictsSomething(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "allkeys-random"
	config.Properties.MaxmemorySamples = 5
	s := NewStandaloneServer()
	defer s.Close()
	db := s.mustSelectDB(0)
	seedKeys(db, 200, false)
	config.Properties.Maxmemory = s.usedMemory() / 2 // force eviction of ~half
	before := db.data.Len()
	if ok := s.freeMemoryIfNeeded(); !ok {
		t.Fatalf("allkeys-random should free memory and return true")
	}
	if db.data.Len() >= before {
		t.Fatalf("no keys evicted: before=%d after=%d", before, db.data.Len())
	}
}

func TestPolicyVolatileOnlyTouchesTTLKeys(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "volatile-lru"
	config.Properties.MaxmemorySamples = 5
	s := NewStandaloneServer()
	defer s.Close()
	db := s.mustSelectDB(0)
	seedKeys(db, 50, false) // no TTL: must be untouchable
	seedKeys(db, 50, true)  // TTL: evictable
	persistentBefore := 0
	db.data.ForEach(func(k string, _ interface{}) bool {
		if _, hasTTL := db.ttlMap.Get(k); !hasTTL {
			persistentBefore++
		}
		return true
	})
	config.Properties.Maxmemory = 1 // force max eviction
	s.freeMemoryIfNeeded()
	persistentAfter := 0
	db.data.ForEach(func(k string, _ interface{}) bool {
		if _, hasTTL := db.ttlMap.Get(k); !hasTTL {
			persistentAfter++
		}
		return true
	})
	if persistentAfter != persistentBefore {
		t.Fatalf("volatile policy evicted persistent keys: before=%d after=%d", persistentBefore, persistentAfter)
	}
}

func TestEvictionCandidatePicksColdestLRU(t *testing.T) {
	config.Properties.MaxmemoryPolicy = "allkeys-lru"
	s := NewStandaloneServer()
	defer s.Close()
	db := s.mustSelectDB(0)
	// hot key touched "now", cold key stamped far in the past
	db.PutEntity("hot", &database.DataEntity{Data: []byte("x")})
	db.PutEntity("cold", &database.DataEntity{Data: []byte("x")})
	db.lruMap.Put("cold", lruMeta{data: (lruClock() - 1000) & lruClockMax})
	db.lruMap.Put("hot", lruMeta{data: lruClock()})
	pool := db.evictionCandidates([]string{"hot", "cold"})
	if pool != "cold" {
		t.Fatalf("LRU should pick the coldest key, got %q", pool)
	}
}
