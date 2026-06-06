package integration

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestGoRedisRESP3Connectivity(t *testing.T) {
	addr, stop := startHayakvProto(t, "resp3")
	defer stop()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr, Protocol: 3}) // sends HELLO 3
	defer client.Close()

	if got := client.Ping(ctx).Val(); got != "PONG" {
		t.Fatalf("PING = %q, want PONG", got)
	}
	if err := client.Set(ctx, "m1:key", "value", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if got := client.Get(ctx, "m1:key").Val(); got != "value" {
		t.Fatalf("GET = %q, want value", got)
	}
	if _, err := client.Get(ctx, "m1:absent").Result(); err != redis.Nil {
		t.Fatalf("GET miss err = %v, want redis.Nil", err)
	}
}
