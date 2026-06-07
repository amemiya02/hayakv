package database

import (
	"sync"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/database"
)

// TestActiveExpireConcurrentWithReads exercises the per-key locking in
// expireIfNeeded against concurrent GetEntity calls. Run with -race.
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
			db.GetEntity("k" + time.Duration(i%200).String())
		}
	}()
	wg.Wait()
}
