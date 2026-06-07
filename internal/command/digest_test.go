package database

import (
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
)

// TestDigestHashEncodingIndependence verifies that a hash with the same
// logical content produces the same value-digest regardless of whether it is
// stored in listpack or hashtable encoding.
func TestDigestHashEncodingIndependence(t *testing.T) {
	db := makeTestDB()

	// Create a small hash with 2 fields — will use listpack encoding.
	smallKey := "hash:small"
	for i := 0; i < 2; i++ {
		field := fmt.Sprintf("field%d", i)
		value := fmt.Sprintf("value%d", i)
		db.Exec(nil, utils.ToCmdLine("HSET", smallKey, field, value))
	}

	entitySmall, ok := db.GetEntity(smallKey)
	if !ok {
		t.Fatal("smallKey not found")
	}
	if h, ok := entitySmall.Data.(*object.Hash); ok {
		if !h.IsListpack() {
			t.Fatal("expected small hash to be listpack")
		}
	}

	// Create a hash on a different key that starts with the same 2 fields
	// but expands to force hashtable encoding, then removes extras.
	largeKey := "hash:large"
	for i := 0; i < 2; i++ {
		field := fmt.Sprintf("field%d", i)
		value := fmt.Sprintf("value%d", i)
		db.Exec(nil, utils.ToCmdLine("HSET", largeKey, field, value))
	}
	for i := 2; i < 200; i++ {
		db.Exec(nil, utils.ToCmdLine("HSET", largeKey, fmt.Sprintf("extra%d", i), fmt.Sprintf("v%d", i)))
	}
	// Verify it converted to hashtable.
	entityLarge, ok := db.GetEntity(largeKey)
	if !ok {
		t.Fatal("largeKey not found")
	}
	if h, ok := entityLarge.Data.(*object.Hash); ok {
		if h.IsListpack() {
			t.Fatal("expected large hash to be hashtable")
		}
	}
	// Remove extras.
	for i := 2; i < 200; i++ {
		db.Exec(nil, utils.ToCmdLine("HDEL", largeKey, fmt.Sprintf("extra%d", i)))
	}

	// Both hashes now have the same 2 fields. Compare their value-only digests.
	entitySmall, _ = db.GetEntity(smallKey)
	entityLarge, _ = db.GetEntity(largeKey)

	// Extract value digest by hashing just the sorted field-value content.
	smallValDigest := hashValueDigest(entitySmall)
	largeValDigest := hashValueDigest(entityLarge)

	if smallValDigest != largeValDigest {
		t.Errorf("encoding-dependent hash value digest: listpack=%x, hashtable=%x",
			smallValDigest, largeValDigest)
	}
}

// TestDigestStringEncodingIndependence verifies that string values produce
// stable digests.
func TestDigestStringEncodingIndependence(t *testing.T) {
	db := makeTestDB()

	db.Exec(nil, utils.ToCmdLine("SET", "key1", "42"))
	entity1, ok := db.GetEntity("key1")
	if !ok {
		t.Fatal("key1 not found")
	}
	d1 := digestKey("key1", entity1, nil)

	db.Exec(nil, utils.ToCmdLine("SET", "key1", "42"))
	entity2, ok := db.GetEntity("key1")
	if !ok {
		t.Fatal("key1 not found after re-set")
	}
	d2 := digestKey("key1", entity2, nil)

	if d1 != d2 {
		t.Errorf("same value, different digest: %x vs %x", d1, d2)
	}

	// Different value should produce different digest.
	db.Exec(nil, utils.ToCmdLine("SET", "key1", "99"))
	entity3, ok := db.GetEntity("key1")
	if !ok {
		t.Fatal("key1 not found after set to 99")
	}
	d3 := digestKey("key1", entity3, nil)
	if d1 == d3 {
		t.Error("different values produced same digest")
	}
}

// TestDigestAllTypes verifies that each Redis type produces a non-zero digest.
func TestDigestAllTypes(t *testing.T) {
	db := makeTestDB()

	db.Exec(nil, utils.ToCmdLine("SET", "str", "hello"))
	db.Exec(nil, utils.ToCmdLine("RPUSH", "lst", "a", "b", "c"))
	db.Exec(nil, utils.ToCmdLine("SADD", "set", "x", "y", "z"))
	db.Exec(nil, utils.ToCmdLine("HSET", "hsh", "f1", "v1", "f2", "v2"))
	db.Exec(nil, utils.ToCmdLine("ZADD", "zset", "1", "m1", "2", "m2", "3", "m3"))

	zero := [20]byte{}
	tests := []struct {
		name string
		key  string
	}{
		{"string", "str"},
		{"list", "lst"},
		{"set", "set"},
		{"hash", "hsh"},
		{"zset", "zset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, ok := db.GetEntity(tt.key)
			if !ok {
				t.Fatalf("key %q not found", tt.key)
			}
			d := digestKey(tt.key, entity, nil)
			if d == zero {
				t.Errorf("digest for %q is all zeros", tt.name)
			}
		})
	}
}

// TestDigestSetEncodingIndependence verifies that a set produces the same
// value-digest regardless of encoding (intset vs hashtable).
func TestDigestSetEncodingIndependence(t *testing.T) {
	db := makeTestDB()

	// Small set of integers — intset encoding.
	smallKey := "set:small"
	db.Exec(nil, utils.ToCmdLine("SADD", smallKey, "1", "2", "3"))
	entitySmall, ok := db.GetEntity(smallKey)
	if !ok {
		t.Fatal("smallKey not found")
	}
	if s, ok := entitySmall.Data.(*object.Set); ok {
		if s.Encoding() != "intset" {
			t.Fatalf("expected intset, got %s", s.Encoding())
		}
	}

	// Large set that forces hashtable, then shrinks to same content.
	largeKey := "set:large"
	db.Exec(nil, utils.ToCmdLine("SADD", largeKey, "1", "2", "3"))
	for i := 0; i < 200; i++ {
		db.Exec(nil, utils.ToCmdLine("SADD", largeKey, fmt.Sprintf("x%d", i)))
	}
	for i := 0; i < 200; i++ {
		db.Exec(nil, utils.ToCmdLine("SREM", largeKey, fmt.Sprintf("x%d", i)))
	}
	entityLarge, ok := db.GetEntity(largeKey)
	if !ok {
		t.Fatal("largeKey not found")
	}

	// Both sets should contain the same members.
	smallMembers := collectSetMembers(entitySmall)
	largeMembers := collectSetMembers(entityLarge)
	if len(smallMembers) != len(largeMembers) {
		t.Fatalf("member count mismatch: small=%d, large=%d", len(smallMembers), len(largeMembers))
	}
	for m := range smallMembers {
		if !largeMembers[m] {
			t.Errorf("member %q missing from large set", m)
		}
	}

	// Compute value digests — should be identical since content is the same.
	smallValDigest := setDigest(entitySmall)
	largeValDigest := setDigest(entityLarge)
	if smallValDigest != largeValDigest {
		t.Errorf("encoding-dependent set value digest: intset=%x, hashtable=%x",
			smallValDigest, largeValDigest)
	}
}

// TestDigestListOrderSensitive verifies that lists with different element
// orders produce different value-only digests.
func TestDigestListOrderSensitive(t *testing.T) {
	db := makeTestDB()

	db.Exec(nil, utils.ToCmdLine("RPUSH", "list1", "a", "b", "c"))
	db.Exec(nil, utils.ToCmdLine("RPUSH", "list2", "c", "b", "a"))

	entity1, _ := db.GetEntity("list1")
	entity2, _ := db.GetEntity("list2")

	d1 := listDigest(entity1)
	d2 := listDigest(entity2)

	if d1 == d2 {
		t.Error("different list orderings produced the same value digest")
	}
}

// TestDigestZSetEncodingIndependence verifies that a zset produces the same
// value-digest regardless of encoding (listpack vs skiplist).
func TestDigestZSetEncodingIndependence(t *testing.T) {
	db := makeTestDB()

	// Small zset — listpack encoding.
	smallKey := "zset:small"
	db.Exec(nil, utils.ToCmdLine("ZADD", smallKey, "1.5", "a", "2.5", "b"))
	entitySmall, ok := db.GetEntity(smallKey)
	if !ok {
		t.Fatal("smallKey not found")
	}
	if z, ok := entitySmall.Data.(*object.ZSet); ok {
		if !z.IsListpack() {
			t.Fatal("expected small zset to be listpack")
		}
	}

	// Large zset that forces skiplist, then shrinks to same content.
	largeKey := "zset:large"
	db.Exec(nil, utils.ToCmdLine("ZADD", largeKey, "1.5", "a", "2.5", "b"))
	for i := 0; i < 200; i++ {
		score := strconv.FormatFloat(float64(i)+100.5, 'f', -1, 64)
		db.Exec(nil, utils.ToCmdLine("ZADD", largeKey, score, fmt.Sprintf("m%d", i)))
	}
	for i := 0; i < 200; i++ {
		db.Exec(nil, utils.ToCmdLine("ZREM", largeKey, fmt.Sprintf("m%d", i)))
	}
	entityLarge, ok := db.GetEntity(largeKey)
	if !ok {
		t.Fatal("largeKey not found")
	}

	// Both zsets should contain the same members with same scores.
	smallEntries := collectZSetEntries(entitySmall)
	largeEntries := collectZSetEntries(entityLarge)
	if len(smallEntries) != len(largeEntries) {
		t.Fatalf("entry count mismatch: small=%d, large=%d", len(smallEntries), len(largeEntries))
	}
	for m, s := range smallEntries {
		if ls, ok := largeEntries[m]; !ok {
			t.Errorf("member %q missing from large zset", m)
		} else if ls != s {
			t.Errorf("member %q score mismatch: small=%f, large=%f", m, s, ls)
		}
	}

	// Compute value digests — should be identical.
	smallValDigest := zsetDigest(entitySmall)
	largeValDigest := zsetDigest(entityLarge)
	if smallValDigest != largeValDigest {
		t.Errorf("encoding-dependent zset value digest: listpack=%x, skiplist=%x",
			smallValDigest, largeValDigest)
	}
}

// TestDigestHashPairBinding verifies that hashes with swapped field-value
// pairings produce different value-only digests. Uses hashValueDigest to
// exclude the key name, so the test isolates the pairing logic.
func TestDigestHashPairBinding(t *testing.T) {
	db := makeTestDB()

	// Hash {a:1, b:2}
	db.Exec(nil, utils.ToCmdLine("HSET", "h1", "a", "1", "b", "2"))
	// Hash {a:2, b:1} — same fields, same values, different pairing
	db.Exec(nil, utils.ToCmdLine("HSET", "h2", "a", "2", "b", "1"))

	e1, _ := db.GetEntity("h1")
	e2, _ := db.GetEntity("h2")

	d1 := hashValueDigest(e1)
	d2 := hashValueDigest(e2)

	if d1 == d2 {
		t.Error("swapped hash pairings produced the same value digest — pairing is not bound")
	}
}

// TestDigestListPairBinding verifies that lists with different element
// orders produce different value-only digests. Uses listDigest to exclude
// the key name, so the test isolates the index↔value binding.
func TestDigestListPairBinding(t *testing.T) {
	db := makeTestDB()

	db.Exec(nil, utils.ToCmdLine("RPUSH", "samekey", "x", "y"))
	db.Exec(nil, utils.ToCmdLine("RPUSH", "samekey2", "y", "x"))

	e1, _ := db.GetEntity("samekey")
	e2, _ := db.GetEntity("samekey2")

	d1 := listDigest(e1)
	d2 := listDigest(e2)

	if d1 == d2 {
		t.Error("swapped list elements produced the same value digest — order is not bound")
	}
}

// TestDigestZSetPairBinding verifies that zsets with swapped member-score
// pairings produce different value-only digests. Uses zsetDigest to exclude
// the key name, so the test isolates the pairing logic.
func TestDigestZSetPairBinding(t *testing.T) {
	db := makeTestDB()

	// ZSet {a:1, b:2}
	db.Exec(nil, utils.ToCmdLine("ZADD", "z1", "1", "a", "2", "b"))
	// ZSet {a:2, b:1} — same members, different scores
	db.Exec(nil, utils.ToCmdLine("ZADD", "z2", "2", "a", "1", "b"))

	e1, _ := db.GetEntity("z1")
	e2, _ := db.GetEntity("z2")

	d1 := zsetDigest(e1)
	d2 := zsetDigest(e2)

	if d1 == d2 {
		t.Error("swapped zset score pairings produced the same value digest")
	}
}

// TestDigestTTLMixed verifies that TTL affects the digest.
func TestDigestTTLMixed(t *testing.T) {
	db := makeTestDB()

	db.Exec(nil, utils.ToCmdLine("SET", "ttlkey", "value"))
	entity, ok := db.GetEntity("ttlkey")
	if !ok {
		t.Fatal("ttlkey not found")
	}
	dNoTTL := digestKey("ttlkey", entity, nil)

	// Set with TTL.
	db.Exec(nil, utils.ToCmdLine("SET", "ttlkey", "value", "EX", "100"))
	entity2, ok := db.GetEntity("ttlkey")
	if !ok {
		t.Fatal("ttlkey not found after SET with EX")
	}
	var expiration *time.Time
	raw, ok := db.ttlMap.Get("ttlkey")
	if ok {
		exp := raw.(time.Time)
		expiration = &exp
	}
	dWithTTL := digestKey("ttlkey", entity2, expiration)

	if dNoTTL == dWithTTL {
		t.Error("TTL did not affect digest")
	}
}

// --- helpers for extracting value-only digests ---

// getHashFromEntity unwraps Robj if present and returns the Hash.
func getHashFromEntity(entity *database.DataEntity) *object.Hash {
	switch v := entity.Data.(type) {
	case *object.Robj:
		return v.Ptr.(*object.Hash)
	case *object.Hash:
		return v
	default:
		panic("unexpected type for hash")
	}
}

// getSetFromEntity unwraps Robj if present and returns the Set.
func getSetFromEntity(entity *database.DataEntity) *object.Set {
	switch v := entity.Data.(type) {
	case *object.Robj:
		return v.Ptr.(*object.Set)
	case *object.Set:
		return v
	default:
		panic("unexpected type for set")
	}
}

// getZSetFromEntity unwraps Robj if present and returns the ZSet.
func getZSetFromEntity(entity *database.DataEntity) *object.ZSet {
	switch v := entity.Data.(type) {
	case *object.Robj:
		return v.Ptr.(*object.ZSet)
	case *object.ZSet:
		return v
	default:
		panic("unexpected type for zset")
	}
}

// getListFromEntity unwraps Robj if present and returns the List.
func getListFromEntity(entity *database.DataEntity) *object.List {
	switch v := entity.Data.(type) {
	case *object.Robj:
		return v.Ptr.(*object.List)
	case *object.List:
		return v
	default:
		panic("unexpected type for list")
	}
}

func collectSetMembers(entity *database.DataEntity) map[string]bool {
	members := make(map[string]bool)
	getSetFromEntity(entity).ForEach(func(member string) bool {
		members[member] = true
		return true
	})
	return members
}

func collectZSetEntries(entity *database.DataEntity) map[string]float64 {
	entries := make(map[string]float64)
	getZSetFromEntity(entity).ForEach(func(member string, score float64) bool {
		entries[member] = score
		return true
	})
	return entries
}

// listDigest computes a value-only digest of a list's elements (order-sensitive).
func listDigest(entity *database.DataEntity) [20]byte {
	var d [20]byte
	getListFromEntity(entity).ForEach(func(i int, val interface{}) bool {
		valBytes := valueToBytes(val)
		blob := append(append([]byte(strconv.Itoa(i)), 0), valBytes...)
		mixDigest(&d, sha1Bytes(blob))
		return true
	})
	return d
}

// setDigest computes a digest of the set's members (sorted, order-independent).
func setDigest(entity *database.DataEntity) [20]byte {
	var d [20]byte
	var members []string
	getSetFromEntity(entity).ForEach(func(member string) bool {
		members = append(members, member)
		return true
	})
	sort.Strings(members)
	for _, m := range members {
		mixDigest(&d, sha1Bytes([]byte(m)))
	}
	return d
}

// zsetDigest computes a digest of the zset's member-score pairs (sorted by member).
func zsetDigest(entity *database.DataEntity) [20]byte {
	var d [20]byte
	type pair struct {
		member string
		score  float64
	}
	var pairs []pair
	getZSetFromEntity(entity).ForEach(func(member string, score float64) bool {
		pairs = append(pairs, pair{member, score})
		return true
	})
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].member < pairs[j].member
	})
	for _, p := range pairs {
		scoreStr := strconv.FormatFloat(p.score, 'f', -1, 64)
		blob := append(append([]byte(p.member), 0), []byte(scoreStr)...)
		mixDigest(&d, sha1Bytes(blob))
	}
	return d
}

// hashValueDigest computes a digest of the hash's field-value pairs (sorted by field).
func hashValueDigest(entity *database.DataEntity) [20]byte {
	var d [20]byte
	type pair struct {
		field string
		value []byte
	}
	var pairs []pair
	// Unwrap Robj if present.
	hash := getHashFromEntity(entity)
	hash.ForEach(func(field string, value interface{}) bool {
		pairs = append(pairs, pair{field, valueToBytes(value)})
		return true
	})
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].field < pairs[j].field
	})
	for _, p := range pairs {
		blob := append(append([]byte(p.field), 0), p.value...)
		mixDigest(&d, sha1Bytes(blob))
	}
	return d
}
