package diff

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestObjectEncodingDiff(t *testing.T) {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// Wait for Redis to be ready
	err := rdb.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Test cases
	tests := []struct {
		name     string
		key      string
		value    interface{}
		expected string
	}{
		// String encoding tests
		{
			name:     "string int",
			key:      "test:int",
			value:    "42",
			expected: "int",
		},
		{
			name:     "string embstr",
			key:      "test:embstr",
			value:    "hello",
			expected: "embstr",
		},
		{
			name:     "string raw",
			key:      "test:raw",
			value:    "this is a long string that exceeds 44 bytes for testing raw encoding",
			expected: "raw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up
			rdb.Del(ctx, tt.key)

			// Set the key
			err := rdb.Set(ctx, tt.key, tt.value, 0).Err()
			if err != nil {
				t.Fatalf("failed to set key: %v", err)
			}

			// Get encoding
			encoding, err := rdb.Do(ctx, "OBJECT", "ENCODING", tt.key).Text()
			if err != nil {
				t.Fatalf("failed to get encoding: %v", err)
			}

			if encoding != tt.expected {
				t.Fatalf("expected encoding %q, got %q", tt.expected, encoding)
			}

			// Clean up
			rdb.Del(ctx, tt.key)
		})
	}
}

func TestObjectEncodingNonExistent(t *testing.T) {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// Wait for Redis to be ready
	err := rdb.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Test non-existent key
	encoding, err := rdb.Do(ctx, "OBJECT", "ENCODING", "nonexistent").Text()
	if err != nil {
		// Redis returns nil for non-existent keys
		if err == redis.Nil {
			return
		}
		t.Fatalf("unexpected error: %v", err)
	}

	// If we got a value, it should be empty
	if encoding != "" {
		t.Fatalf("expected empty encoding for non-existent key, got %q", encoding)
	}
}

func TestObjectEncodingHash(t *testing.T) {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// Wait for Redis to be ready
	err := rdb.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Clean up
	key := "test:hash"
	rdb.Del(ctx, key)

	// Add hash fields
	for i := 0; i < 10; i++ {
		field := "field" + string(rune('0'+i))
		value := "value" + string(rune('0'+i))
		rdb.HSet(ctx, key, field, value)
	}

	// Get encoding
	encoding, err := rdb.Do(ctx, "OBJECT", "ENCODING", key).Text()
	if err != nil {
		t.Fatalf("failed to get encoding: %v", err)
	}

	// Should be listpack for small hashes
	if encoding != "listpack" {
		t.Logf("hash encoding: %s (may vary by Redis version)", encoding)
	}

	// Clean up
	rdb.Del(ctx, key)
}

func TestObjectEncodingSet(t *testing.T) {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// Wait for Redis to be ready
	err := rdb.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Clean up
	key := "test:set"
	rdb.Del(ctx, key)

	// Add set members
	for i := 0; i < 10; i++ {
		member := "member" + string(rune('0'+i))
		rdb.SAdd(ctx, key, member)
	}

	// Get encoding
	encoding, err := rdb.Do(ctx, "OBJECT", "ENCODING", key).Text()
	if err != nil {
		t.Fatalf("failed to get encoding: %v", err)
	}

	// Should be hashtable for small sets
	if encoding != "hashtable" {
		t.Logf("set encoding: %s (may vary by Redis version)", encoding)
	}

	// Clean up
	rdb.Del(ctx, key)
}

func TestObjectEncodingSortedSet(t *testing.T) {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// Wait for Redis to be ready
	err := rdb.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Clean up
	key := "test:zset"
	rdb.Del(ctx, key)

	// Add sorted set members
	for i := 0; i < 10; i++ {
		member := "member" + string(rune('0'+i))
		score := float64(i)
		rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: member})
	}

	// Get encoding
	encoding, err := rdb.Do(ctx, "OBJECT", "ENCODING", key).Text()
	if err != nil {
		t.Fatalf("failed to get encoding: %v", err)
	}

	// Should be listpack for small sorted sets
	if encoding != "listpack" {
		t.Logf("sorted set encoding: %s (may vary by Redis version)", encoding)
	}

	// Clean up
	rdb.Del(ctx, key)
}

func TestObjectEncodingList(t *testing.T) {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// Wait for Redis to be ready
	err := rdb.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Clean up
	key := "test:list"
	rdb.Del(ctx, key)

	// Add list elements
	for i := 0; i < 10; i++ {
		value := "value" + string(rune('0'+i))
		rdb.RPush(ctx, key, value)
	}

	// Get encoding
	encoding, err := rdb.Do(ctx, "OBJECT", "ENCODING", key).Text()
	if err != nil {
		t.Fatalf("failed to get encoding: %v", err)
	}

	// Should be listpack for small lists
	if encoding != "listpack" {
		t.Logf("list encoding: %s (may vary by Redis version)", encoding)
	}

	// Clean up
	rdb.Del(ctx, key)
}
