package integration

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestScriptEffectsReplicate(t *testing.T) {
	masterAddr, stopM := startHayakvRepl(t, "")
	defer stopM()
	replicaAddr, stopR := startHayakvRepl(t, "")
	defer stopR()

	ctx := context.Background()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	replica := redis.NewClient(&redis.Options{Addr: replicaAddr, Protocol: 2})
	defer replica.Close()

	// Attach replica.
	mhost, mport := splitAddr(t, masterAddr)
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("REPLICAOF: %v", err)
	}

	// Warm the link with a plain SET.
	if err := master.Set(ctx, "warm", "1", 0).Err(); err != nil {
		t.Fatalf("warm SET: %v", err)
	}
	pollGet(t, replica, "warm", "1", 10*time.Second)

	// Use EVAL to SET a key on the master; the effect must replicate.
	result, err := master.Eval(ctx, "redis.call('set', KEYS[1], ARGV[1]) return redis.call('get', KEYS[1])", []string{"esk"}, "esv").Result()
	if err != nil {
		t.Fatalf("EVAL: %v", err)
	}
	if result != "esv" {
		t.Fatalf("EVAL result = %v, want esv", result)
	}

	// The key set by the script must appear on the replica.
	pollGet(t, replica, "esk", "esv", 10*time.Second)
}
