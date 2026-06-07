package database

import (
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/database"
)

func TestActiveExpireReapsWithoutRead(t *testing.T) {
	db := makeBasicDB()
	// 50 keys all already expired (expireAt in the past)
	past := time.Now().Add(-time.Hour)
	for i := 0; i < 50; i++ {
		k := "k" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		db.PutEntity(k, &database.DataEntity{Data: []byte("v")})
		db.ttlMap.Put(k, past) // set ttl WITHOUT scheduling a timewheel timer
	}
	if db.data.Len() != 50 {
		t.Fatalf("setup: data len = %d", db.data.Len())
	}
	// one full cycle should reap all expired sampled keys (loops while ratio>25%)
	reaped := db.activeExpireCycle(activeExpireConfig{sampleSize: 20, maxLoops: 64})
	if reaped == 0 {
		t.Fatalf("active expire reaped 0")
	}
	if db.data.Len() != 0 {
		t.Fatalf("after active expire, data len = %d, want 0", db.data.Len())
	}
}

func TestActiveExpireSkipsLiveKeys(t *testing.T) {
	db := makeBasicDB()
	future := time.Now().Add(time.Hour)
	db.PutEntity("live", &database.DataEntity{Data: []byte("v")})
	db.ttlMap.Put("live", future)
	db.PutEntity("noTTL", &database.DataEntity{Data: []byte("v")})
	db.activeExpireCycle(activeExpireConfig{sampleSize: 20, maxLoops: 8})
	if _, ok := db.data.Get("live"); !ok {
		t.Fatalf("live key was wrongly reaped")
	}
	if _, ok := db.data.Get("noTTL"); !ok {
		t.Fatalf("no-ttl key was wrongly reaped")
	}
}
