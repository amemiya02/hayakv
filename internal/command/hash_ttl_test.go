package database

import (
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"testing"
)

func TestHGetEx(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	// Set up a hash with two fields
	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1"))
	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f2", "v2"))

	// HGETEX without TTL option - just returns values
	result := testDB.Exec(nil, utils.ToCmdLine("hgetex", key, "FIELDS", "2", "f1", "f2"))
	multiBulk, ok := result.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %s", string(result.ToBytes()))
	}
	if len(multiBulk.Args) != 2 {
		t.Fatalf("expected 2 results, got %d", len(multiBulk.Args))
	}
	if string(multiBulk.Args[0]) != "v1" {
		t.Fatalf("expected v1, got %s", string(multiBulk.Args[0]))
	}
	if string(multiBulk.Args[1]) != "v2" {
		t.Fatalf("expected v2, got %s", string(multiBulk.Args[1]))
	}

	// HGETEX on non-existent key
	result = testDB.Exec(nil, utils.ToCmdLine("hgetex", "nokey", "FIELDS", "1", "f1"))
	multiBulk, ok = result.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %s", string(result.ToBytes()))
	}
	if len(multiBulk.Args) != 1 {
		t.Fatalf("expected 1 result, got %d", len(multiBulk.Args))
	}
	if multiBulk.Args[0] != nil {
		t.Fatalf("expected nil, got %s", string(multiBulk.Args[0]))
	}

	// HGETEX with EX option
	result = testDB.Exec(nil, utils.ToCmdLine("hgetex", key, "EX", "100", "FIELDS", "1", "f1"))
	multiBulk, ok = result.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %s", string(result.ToBytes()))
	}
	if string(multiBulk.Args[0]) != "v1" {
		t.Fatalf("expected v1, got %s", string(multiBulk.Args[0]))
	}

	// HGETEX with PERSIST option
	result = testDB.Exec(nil, utils.ToCmdLine("hgetex", key, "PERSIST", "FIELDS", "1", "f1"))
	multiBulk, ok = result.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %s", string(result.ToBytes()))
	}
	if string(multiBulk.Args[0]) != "v1" {
		t.Fatalf("expected v1, got %s", string(multiBulk.Args[0]))
	}
}

func TestHGetDel(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	// Set up a hash
	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1"))
	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f2", "v2"))

	// HGETDEL - get and delete f1
	result := testDB.Exec(nil, utils.ToCmdLine("hgetdel", key, "FIELDS", "1", "f1"))
	multiBulk, ok := result.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %s", string(result.ToBytes()))
	}
	if len(multiBulk.Args) != 1 {
		t.Fatalf("expected 1 result, got %d", len(multiBulk.Args))
	}
	if string(multiBulk.Args[0]) != "v1" {
		t.Fatalf("expected v1, got %s", string(multiBulk.Args[0]))
	}

	// Verify f1 is deleted
	result = testDB.Exec(nil, utils.ToCmdLine("hexists", key, "f1"))
	if intResult, _ := result.(*protocol.IntReply); intResult.Code != 0 {
		t.Fatal("HGETDEL did not delete field")
	}

	// Verify f2 still exists
	result = testDB.Exec(nil, utils.ToCmdLine("hexists", key, "f2"))
	if intResult, _ := result.(*protocol.IntReply); intResult.Code != 1 {
		t.Fatal("HGETDEL deleted wrong field")
	}

	// HGETDEL on non-existent key
	result = testDB.Exec(nil, utils.ToCmdLine("hgetdel", "nokey", "FIELDS", "1", "f1"))
	multiBulk, ok = result.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %s", string(result.ToBytes()))
	}
	if multiBulk.Args[0] != nil {
		t.Fatalf("expected nil, got %s", string(multiBulk.Args[0]))
	}

	// HGETDEL all remaining fields - should remove the key
	result = testDB.Exec(nil, utils.ToCmdLine("hgetdel", key, "FIELDS", "1", "f2"))
	multiBulk, ok = result.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %s", string(result.ToBytes()))
	}
	if string(multiBulk.Args[0]) != "v2" {
		t.Fatalf("expected v2, got %s", string(multiBulk.Args[0]))
	}

	// Key should be removed when hash is empty
	result = testDB.Exec(nil, utils.ToCmdLine("hlen", key))
	if intResult, _ := result.(*protocol.IntReply); intResult.Code != 0 {
		t.Fatal("HGETDEL should remove key when hash is empty")
	}
}

func TestHSetEx(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	// HSETEX basic - set field-value pairs
	result := testDB.Exec(nil, utils.ToCmdLine("hsetex", key, "FIELDS", "2", "f1", "v1", "f2", "v2"))
	if intResult, _ := result.(*protocol.IntReply); intResult.Code != 2 {
		t.Fatalf("expected 2, got %d", intResult.Code)
	}

	// Verify values
	result = testDB.Exec(nil, utils.ToCmdLine("hget", key, "f1"))
	if bulkResult, _ := result.(*protocol.BulkReply); string(bulkResult.Arg) != "v1" {
		t.Fatalf("expected v1, got %s", string(bulkResult.Arg))
	}

	// HSETEX with FNX - only set if field doesn't exist
	result = testDB.Exec(nil, utils.ToCmdLine("hsetex", key, "FNX", "FIELDS", "2", "f1", "new1", "f3", "v3"))
	if intResult, _ := result.(*protocol.IntReply); intResult.Code != 1 {
		t.Fatalf("FNX: expected 1 (only f3), got %d", intResult.Code)
	}

	// f1 should still be v1
	result = testDB.Exec(nil, utils.ToCmdLine("hget", key, "f1"))
	if bulkResult, _ := result.(*protocol.BulkReply); string(bulkResult.Arg) != "v1" {
		t.Fatalf("FNX: expected v1, got %s", string(bulkResult.Arg))
	}

	// HSETEX with FXX - only set if field exists
	result = testDB.Exec(nil, utils.ToCmdLine("hsetex", key, "FXX", "FIELDS", "2", "f1", "updated1", "f4", "v4"))
	if intResult, _ := result.(*protocol.IntReply); intResult.Code != 1 {
		t.Fatalf("FXX: expected 1 (only f1), got %d", intResult.Code)
	}

	// f1 should be updated
	result = testDB.Exec(nil, utils.ToCmdLine("hget", key, "f1"))
	if bulkResult, _ := result.(*protocol.BulkReply); string(bulkResult.Arg) != "updated1" {
		t.Fatalf("FXX: expected updated1, got %s", string(bulkResult.Arg))
	}

	// HSETEX with EX option
	result = testDB.Exec(nil, utils.ToCmdLine("hsetex", key, "EX", "100", "FIELDS", "1", "f5", "v5"))
	if intResult, _ := result.(*protocol.IntReply); intResult.Code != 1 {
		t.Fatalf("EX: expected 1, got %d", intResult.Code)
	}
}

func TestHGetExSyntax(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	// Wrong type key
	testDB.Exec(nil, utils.ToCmdLine("set", key, "val"))
	result := testDB.Exec(nil, utils.ToCmdLine("hgetex", key, "FIELDS", "1", "f"))
	if _, ok := result.(*protocol.WrongTypeErrReply); !ok {
		t.Fatalf("expected wrong type error, got %s", string(result.ToBytes()))
	}
}

func TestHExpireHTTL(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	// Set up a hash with two fields
	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1"))
	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f2", "v2"))

	// HEXPIRE on f1 with 100 seconds
	result := testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "100", "FIELDS", "1", "f1"))
	raw := result.ToBytes()
	if string(raw) != "*1\r\n:1\r\n" {
		t.Fatalf("HEXPIRE = %q", raw)
	}

	// HTTL on both fields - f1 should have TTL, f2 should not
	result = testDB.Exec(nil, utils.ToCmdLine("httl", key, "FIELDS", "2", "f1", "f2"))
	multiRaw, ok := result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("expected MultiRawReply, got %T: %s", result, string(result.ToBytes()))
	}
	if len(multiRaw.Replies) != 2 {
		t.Fatalf("expected 2 results, got %d", len(multiRaw.Replies))
	}
	// f1 should have a positive TTL
	f1TTL := multiRaw.Replies[0].(*protocol.IntReply).Code
	if f1TTL <= 0 || f1TTL > 100 {
		t.Fatalf("f1 TTL expected 1-100, got %d", f1TTL)
	}
	// f2 should have no TTL (-1)
	f2TTL := multiRaw.Replies[1].(*protocol.IntReply).Code
	if f2TTL != -1 {
		t.Fatalf("f2 TTL expected -1, got %d", f2TTL)
	}

	// HTTL on non-existent field should return -2
	result = testDB.Exec(nil, utils.ToCmdLine("httl", key, "FIELDS", "1", "nosuchfield"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != -2 {
		t.Fatalf("expected -2 for missing field, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// HTTL on non-existent key should return -2 for all fields
	result = testDB.Exec(nil, utils.ToCmdLine("httl", "nokey", "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != -2 {
		t.Fatalf("expected -2 for missing key, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}
}

func TestHPExpireHPTTL(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1"))

	// HPEXPIRE with 60000ms
	result := testDB.Exec(nil, utils.ToCmdLine("hpexpire", key, "60000", "FIELDS", "1", "f1"))
	raw := result.ToBytes()
	if string(raw) != "*1\r\n:1\r\n" {
		t.Fatalf("HPEXPIRE = %q", raw)
	}

	// HPTTL should return ~60000
	result = testDB.Exec(nil, utils.ToCmdLine("hpttl", key, "FIELDS", "1", "f1"))
	multiRaw := result.(*protocol.MultiRawReply)
	ttl := multiRaw.Replies[0].(*protocol.IntReply).Code
	if ttl <= 59000 || ttl > 60000 {
		t.Fatalf("HPTTL expected ~60000, got %d", ttl)
	}
}

func TestHExpireAtHExpireTime(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1"))

	// Set expiry far in the future (year 2099)
	farFuture := int64(4070908800) // 2099-01-01 in unix seconds
	result := testDB.Exec(nil, utils.ToCmdLine("hexpireat", key, "4070908800", "FIELDS", "1", "f1"))
	raw := result.ToBytes()
	if string(raw) != "*1\r\n:1\r\n" {
		t.Fatalf("HEXPIREAT = %q", raw)
	}

	// HEXPIRETIME should return the timestamp
	result = testDB.Exec(nil, utils.ToCmdLine("hexpiretime", key, "FIELDS", "1", "f1"))
	multiRaw := result.(*protocol.MultiRawReply)
	ts := multiRaw.Replies[0].(*protocol.IntReply).Code
	if ts != farFuture {
		t.Fatalf("HEXPIRETIME expected %d, got %d", farFuture, ts)
	}

	// HPEXPIREAT + HPEXPIRETIME
	farFutureMs := farFuture * 1000
	result = testDB.Exec(nil, utils.ToCmdLine("hpexpireat", key, "4070908800000", "FIELDS", "1", "f1"))
	raw = result.ToBytes()
	if string(raw) != "*1\r\n:1\r\n" {
		t.Fatalf("HPEXPIREAT = %q", raw)
	}

	result = testDB.Exec(nil, utils.ToCmdLine("hpexpiretime", key, "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	tsMs := multiRaw.Replies[0].(*protocol.IntReply).Code
	if tsMs != farFutureMs {
		t.Fatalf("HPEXPIRETIME expected %d, got %d", farFutureMs, tsMs)
	}
}

func TestHPersist(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1"))
	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f2", "v2"))

	// Set expiry on f1
	testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "100", "FIELDS", "1", "f1"))

	// HPERSIST on f1 should return 1 (TTL removed)
	result := testDB.Exec(nil, utils.ToCmdLine("hpersist", key, "FIELDS", "1", "f1"))
	multiRaw := result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 1 {
		t.Fatalf("HPERSIST expected 1, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// f1 should now have no TTL
	result = testDB.Exec(nil, utils.ToCmdLine("httl", key, "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != -1 {
		t.Fatalf("HTTL after HPERSIST expected -1, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// HPERSIST on f2 (no TTL) should return -1
	result = testDB.Exec(nil, utils.ToCmdLine("hpersist", key, "FIELDS", "1", "f2"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != -1 {
		t.Fatalf("HPERSIST on field without TTL expected -1, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// HPERSIST on non-existent field should return -2
	result = testDB.Exec(nil, utils.ToCmdLine("hpersist", key, "FIELDS", "1", "nosuch"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != -2 {
		t.Fatalf("HPERSIST on missing field expected -2, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// HPERSIST on non-existent key should return -2
	result = testDB.Exec(nil, utils.ToCmdLine("hpersist", "nokey", "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != -2 {
		t.Fatalf("HPERSIST on missing key expected -2, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}
}

func TestHExpireOptions(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1"))

	// Set initial TTL of 100s
	testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "100", "FIELDS", "1", "f1"))

	// NX should fail (TTL already set) -> 0
	result := testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "200", "NX", "FIELDS", "1", "f1"))
	multiRaw := result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 0 {
		t.Fatalf("NX on existing TTL expected 0, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// XX should succeed (TTL exists) -> 1
	result = testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "200", "XX", "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 1 {
		t.Fatalf("XX on existing TTL expected 1, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// GT with higher value should succeed -> 1
	result = testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "300", "GT", "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 1 {
		t.Fatalf("GT with higher value expected 1, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// GT with lower value should fail -> 0
	result = testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "100", "GT", "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 0 {
		t.Fatalf("GT with lower value expected 0, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// LT with lower value should succeed -> 1
	result = testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "50", "LT", "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 1 {
		t.Fatalf("LT with lower value expected 1, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}

	// LT with higher value should fail -> 0
	result = testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "500", "LT", "FIELDS", "1", "f1"))
	multiRaw = result.(*protocol.MultiRawReply)
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 0 {
		t.Fatalf("LT with higher value expected 0, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}
}

func TestHExpireMultiFields(t *testing.T) {
	testDB.Flush()
	key := utils.RandString(10)

	testDB.Exec(nil, utils.ToCmdLine("hset", key, "f1", "v1", "f2", "v2", "f3", "v3"))

	// HEXPIRE on multiple fields
	result := testDB.Exec(nil, utils.ToCmdLine("hexpire", key, "100", "FIELDS", "3", "f1", "f2", "nosuch"))
	multiRaw := result.(*protocol.MultiRawReply)
	if len(multiRaw.Replies) != 3 {
		t.Fatalf("expected 3 results, got %d", len(multiRaw.Replies))
	}
	if multiRaw.Replies[0].(*protocol.IntReply).Code != 1 {
		t.Fatalf("f1 expected 1, got %d", multiRaw.Replies[0].(*protocol.IntReply).Code)
	}
	if multiRaw.Replies[1].(*protocol.IntReply).Code != 1 {
		t.Fatalf("f2 expected 1, got %d", multiRaw.Replies[1].(*protocol.IntReply).Code)
	}
	if multiRaw.Replies[2].(*protocol.IntReply).Code != -2 {
		t.Fatalf("nosuch expected -2, got %d", multiRaw.Replies[2].(*protocol.IntReply).Code)
	}
}

func TestHExpireNonExistentKey(t *testing.T) {
	testDB.Flush()

	// HEXPIRE on non-existent key should return -2 for all fields
	result := testDB.Exec(nil, utils.ToCmdLine("hexpire", "nokey", "100", "FIELDS", "2", "f1", "f2"))
	multiRaw := result.(*protocol.MultiRawReply)
	for i, r := range multiRaw.Replies {
		if r.(*protocol.IntReply).Code != -2 {
			t.Fatalf("field %d expected -2, got %d", i, r.(*protocol.IntReply).Code)
		}
	}
}
