package diff

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
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

// TestObjectEncodingHayakv verifies OBJECT ENCODING against hayakv itself
// using the redisdb engine with RESP3.
// buildHSetArgs constructs HSET command args with n field-value pairs.
func buildHSetArgs(key string, n int) []string {
	args := []string{"HSET", key}
	for i := 0; i < n; i++ {
		args = append(args, fmt.Sprintf("field%d", i), fmt.Sprintf("value%d", i))
	}
	return args
}

// buildSAddArgs constructs SADD command args with n members.
func buildSAddArgs(key string, n int) []string {
	args := []string{"SADD", key}
	for i := 0; i < n; i++ {
		args = append(args, fmt.Sprintf("member%d", i))
	}
	return args
}

// buildZAddArgs constructs ZADD command args with n score-member pairs.
func buildZAddArgs(key string, n int) []string {
	args := []string{"ZADD", key}
	for i := 0; i < n; i++ {
		args = append(args, fmt.Sprintf("%d", i), fmt.Sprintf("m%d", i))
	}
	return args
}

// buildRPushArgs constructs RPUSH command args with n elements.
func buildRPushArgs(key string, n int) []string {
	args := []string{"RPUSH", key}
	for i := 0; i < n; i++ {
		args = append(args, fmt.Sprintf("val%d", i))
	}
	return args
}

func TestObjectEncodingHayakv(t *testing.T) {
	addr, stop := startHayakvWithConfig(t, "redisdb", "resp3")
	defer stop()

	// Helper: send a raw RESP command and return the reply as a string
	sendRaw := func(t *testing.T, conn net.Conn, reader *bufio.Reader, args ...string) string {
		t.Helper()
		if _, err := conn.Write(encodeCommand(args)); err != nil {
			t.Fatalf("write %v: %v", args, err)
		}
		reply, err := readReply(reader)
		if err != nil {
			t.Fatalf("read %v: %v", args, err)
		}
		return strings.TrimSpace(string(reply))
	}

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// Switch to RESP3
	helloReply := sendRaw(t, conn, reader, "HELLO", "3")
	if !strings.Contains(helloReply, "hayakv") && !strings.Contains(helloReply, "redis") {
		t.Fatalf("unexpected HELLO 3 reply: %q", helloReply)
	}

	// Flush all keys
	sendRaw(t, conn, reader, "FLUSHALL")

	tests := []struct {
		name     string
		setup    []string // commands to run before OBJECT ENCODING
		key      string
		expected string
	}{
		// --- String encodings ---
		{
			name:     "string int",
			setup:    []string{"SET", "test:int", "42"},
			key:      "test:int",
			expected: "int",
		},
		{
			name:     "string embstr",
			setup:    []string{"SET", "test:embstr", "hello"},
			key:      "test:embstr",
			expected: "embstr",
		},
		{
			name:     "string raw",
			setup:    []string{"SET", "test:raw", "this is a long string that exceeds 44 bytes for testing raw encoding"},
			key:      "test:raw",
			expected: "raw",
		},
		// --- Hash encodings ---
		{
			name: "hash listpack",
			setup: []string{
				"HSET", "test:hash-lp", "f1", "v1", "f2", "v2", "f3", "v3",
			},
			key:      "test:hash-lp",
			expected: "listpack",
		},
		{
			name:     "hash hashtable",
			setup:    buildHSetArgs("test:hash-ht", 129),
			key:      "test:hash-ht",
			expected: "hashtable",
		},
		// --- Set encodings ---
		{
			name:     "set intset",
			setup:    []string{"SADD", "test:set-int", "1", "2", "3"},
			key:      "test:set-int",
			expected: "intset",
		},
		{
			name:     "set listpack",
			setup:    []string{"SADD", "test:set-lp", "a", "b", "c"},
			key:      "test:set-lp",
			expected: "listpack",
		},
		{
			name:     "set hashtable",
			setup:    buildSAddArgs("test:set-ht", 129),
			key:      "test:set-ht",
			expected: "hashtable",
		},
		// --- Sorted set encodings ---
		{
			name:     "zset listpack",
			setup:    []string{"ZADD", "test:zset-lp", "1", "a", "2", "b", "3", "c"},
			key:      "test:zset-lp",
			expected: "listpack",
		},
		{
			name:     "zset skiplist",
			setup:    buildZAddArgs("test:zset-sl", 129),
			key:      "test:zset-sl",
			expected: "skiplist",
		},
		// --- List encodings ---
		{
			name:     "list listpack",
			setup:    []string{"RPUSH", "test:list-lp", "a", "b", "c"},
			key:      "test:list-lp",
			expected: "listpack",
		},
		{
			name:     "list quicklist",
			setup:    buildRPushArgs("test:list-ql", 129),
			key:      "test:list-ql",
			expected: "quicklist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up the key first
			sendRaw(t, conn, reader, "DEL", tt.key)

			// Run the setup command
			sendRaw(t, conn, reader, tt.setup...)

			// Query OBJECT ENCODING
			encReply := sendRaw(t, conn, reader, "OBJECT", "ENCODING", tt.key)

			// The reply from readReply is raw RESP; for a bulk string the format is "$N\r\nvalue\r\n"
			// Parse out the value after the CRLF header.
			parts := strings.Split(encReply, "\r\n")
			var got string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" && !strings.HasPrefix(p, "$") {
					got = p
					break
				}
			}
			if got != tt.expected {
				t.Fatalf("expected encoding %q, got %q (raw: %q)", tt.expected, got, encReply)
			}

			// Clean up
			sendRaw(t, conn, reader, "DEL", tt.key)
		})
	}

	// Verify that read commands do NOT change zset encoding from listpack to skiplist.
	// This is the core P1 fix: small zsets must stay in listpack even after ZRANGE, ZRANK, etc.
	t.Run("zset_listpack_survives_reads", func(t *testing.T) {
		key := "test:zset-read-stability"
		sendRaw(t, conn, reader, "DEL", key)

		// Create a small zset in listpack encoding
		sendRaw(t, conn, reader, "ZADD", key, "1", "a", "2", "b", "3", "c")

		// Confirm listpack after ZADD
		encReply := sendRaw(t, conn, reader, "OBJECT", "ENCODING", key)
		parts := strings.Split(encReply, "\r\n")
		var got string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("expected listpack after ZADD, got %q", got)
		}

		// Execute various read commands
		sendRaw(t, conn, reader, "ZRANGE", key, "0", "-1")
		sendRaw(t, conn, reader, "ZREVRANGE", key, "0", "-1")
		sendRaw(t, conn, reader, "ZRANK", key, "a")
		sendRaw(t, conn, reader, "ZREVRANK", key, "c")
		sendRaw(t, conn, reader, "ZCARD", key)
		sendRaw(t, conn, reader, "ZSCORE", key, "b")
		sendRaw(t, conn, reader, "ZCOUNT", key, "-inf", "+inf")
		sendRaw(t, conn, reader, "ZRANGEBYSCORE", key, "-inf", "+inf")
		sendRaw(t, conn, reader, "ZREVRANGEBYSCORE", key, "+inf", "-inf")

		// Encoding must STILL be listpack
		encReply = sendRaw(t, conn, reader, "OBJECT", "ENCODING", key)
		parts = strings.Split(encReply, "\r\n")
		got = ""
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("expected listpack after reads, got %q (reads should not mutate encoding)", got)
		}

		sendRaw(t, conn, reader, "DEL", key)
	})

	// Verify that read commands do NOT change hash encoding from listpack to hashtable.
	t.Run("hash_listpack_survives_reads", func(t *testing.T) {
		key := "test:hash-read-stability"
		sendRaw(t, conn, reader, "DEL", key)

		// Create a small hash in listpack encoding
		sendRaw(t, conn, reader, "HSET", key, "f1", "v1", "f2", "v2", "f3", "v3")

		// Confirm listpack after HSET
		encReply := sendRaw(t, conn, reader, "OBJECT", "ENCODING", key)
		parts := strings.Split(encReply, "\r\n")
		var got string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("expected listpack after HSET, got %q", got)
		}

		// Execute various read commands
		sendRaw(t, conn, reader, "HGET", key, "f1")
		sendRaw(t, conn, reader, "HMGET", key, "f1", "f2", "f3")
		sendRaw(t, conn, reader, "HGETALL", key)
		sendRaw(t, conn, reader, "HKEYS", key)
		sendRaw(t, conn, reader, "HVALS", key)
		sendRaw(t, conn, reader, "HLEN", key)
		sendRaw(t, conn, reader, "HEXISTS", key, "f1")
		sendRaw(t, conn, reader, "HSCAN", key, "0")

		// Encoding must STILL be listpack
		encReply = sendRaw(t, conn, reader, "OBJECT", "ENCODING", key)
		parts = strings.Split(encReply, "\r\n")
		got = ""
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("expected listpack after reads, got %q (reads should not mutate encoding)", got)
		}

		sendRaw(t, conn, reader, "DEL", key)
	})

	// Verify that BGREWRITEAOF (which exercises EntityToCmd) does NOT change
	// hash or zset encoding from listpack to the full-structure encoding.
	t.Run("listpack_survives_aof_rewrite", func(t *testing.T) {
		// --- Hash ---
		hKey := "test:hash-aof-stability"
		sendRaw(t, conn, reader, "DEL", hKey)
		sendRaw(t, conn, reader, "HSET", hKey, "f1", "v1", "f2", "v2", "f3", "v3")

		encReply := sendRaw(t, conn, reader, "OBJECT", "ENCODING", hKey)
		parts := strings.Split(encReply, "\r\n")
		var got string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("hash: expected listpack after HSET, got %q", got)
		}

		// Trigger AOF rewrite (exercises EntityToCmd via the persistence path)
		sendRaw(t, conn, reader, "BGREWRITEAOF")
		// Give it a moment to complete
		time.Sleep(200 * time.Millisecond)

		// Encoding must STILL be listpack
		encReply = sendRaw(t, conn, reader, "OBJECT", "ENCODING", hKey)
		parts = strings.Split(encReply, "\r\n")
		got = ""
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("hash: expected listpack after BGREWRITEAOF, got %q (persistence should not mutate encoding)", got)
		}

		sendRaw(t, conn, reader, "DEL", hKey)

		// --- ZSet ---
		zKey := "test:zset-aof-stability"
		sendRaw(t, conn, reader, "DEL", zKey)
		sendRaw(t, conn, reader, "ZADD", zKey, "1", "a", "2", "b", "3", "c")

		encReply = sendRaw(t, conn, reader, "OBJECT", "ENCODING", zKey)
		parts = strings.Split(encReply, "\r\n")
		got = ""
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("zset: expected listpack after ZADD, got %q", got)
		}

		// Trigger AOF rewrite
		sendRaw(t, conn, reader, "BGREWRITEAOF")
		time.Sleep(200 * time.Millisecond)

		// Encoding must STILL be listpack
		encReply = sendRaw(t, conn, reader, "OBJECT", "ENCODING", zKey)
		parts = strings.Split(encReply, "\r\n")
		got = ""
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "$") {
				got = p
				break
			}
		}
		if got != "listpack" {
			t.Fatalf("zset: expected listpack after BGREWRITEAOF, got %q (persistence should not mutate encoding)", got)
		}

		sendRaw(t, conn, reader, "DEL", zKey)
	})

	// Verify non-existent key returns nil
	conn.Write(encodeCommand([]string{"OBJECT", "ENCODING", "nonexistent"}))
	nonReply, err := readReply(reader)
	if err != nil {
		t.Fatalf("read nonexistent encoding: %v", err)
	}
	// RESP3 null is "_\r\n", RESP2 null is "$-1\r\n"
	if !bytes.Contains(nonReply, []byte("-1")) && !bytes.Contains(nonReply, []byte("_")) {
		t.Fatalf("expected nil for nonexistent key, got %q", nonReply)
	}
}
