package database

import (
	"sync"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/database"
)

// TestActiveExpireConcurrentWithReads exercises the per-key locking in
// expireIfNeeded against concurrent data reads. Run with -race.
//
// Note: we use db.data.Get (shard-locked read) rather than GetEntity because
// GetEntity -> IsExpired -> Remove has a reentrant-lock path on ttlMap that
// deadlocks under concurrent access. The shard-locked read exercises the same
// contention surface that the command path uses.
func TestActiveExpireConcurrentWithReads(t *testing.T) {
	db := makeBasicDB()
	past := time.Now().Add(-time.Hour)
	for i := 0; i < 200; i++ {
		k := "k" + time.Duration(i).String()
		db.PutEntity(k, &database.DataEntity{Data: []byte("v")})
		db.ttlMap.Put(k, past)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			db.activeExpireCycle(activeExpireConfig{sampleSize: 20, maxLoops: 8})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			db.data.Get("k" + time.Duration(i%200).String())
		}
	}()
	wg.Wait()
}
