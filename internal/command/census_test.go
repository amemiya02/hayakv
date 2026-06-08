package database

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
)

func TestLPos(t *testing.T) {
	testDB.Flush()

	// Setup: RPUSH l a b c b
	testDB.Exec(nil, utils.ToCmdLine("RPUSH", "l", "a", "b", "c", "b"))

	// LPOS l b -> first match at index 1
	result := testDB.Exec(nil, utils.ToCmdLine("LPOS", "l", "b"))
	asserts.AssertIntReply(t, result, 1)

	// LPOS l b RANK -1 -> last match at index 3
	result = testDB.Exec(nil, utils.ToCmdLine("LPOS", "l", "b", "RANK", "-1"))
	asserts.AssertIntReply(t, result, 3)

	// LPOS l b RANK 2 -> second match at index 3
	result = testDB.Exec(nil, utils.ToCmdLine("LPOS", "l", "b", "RANK", "2"))
	asserts.AssertIntReply(t, result, 3)

	// LPOS l b RANK 3 -> no third match
	result = testDB.Exec(nil, utils.ToCmdLine("LPOS", "l", "b", "RANK", "3"))
	if string(result.ToBytes()) != "$-1\r\n" {
		t.Fatalf("LPOS rank 3 (no match): %q", result.ToBytes())
	}

	// LPOS l b COUNT 0 -> all matches as array [1, 3]
	result = testDB.Exec(nil, utils.ToCmdLine("LPOS", "l", "b", "COUNT", "0"))
	multiRaw, ok := result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("LPOS COUNT 0: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	if len(multiRaw.Replies) != 2 {
		t.Fatalf("LPOS COUNT 0: expected 2 results, got %d", len(multiRaw.Replies))
	}

	// LPOS l b COUNT 1 -> first match [1]
	result = testDB.Exec(nil, utils.ToCmdLine("LPOS", "l", "b", "COUNT", "1"))
	multiRaw, ok = result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("LPOS COUNT 1: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	if len(multiRaw.Replies) != 1 {
		t.Fatalf("LPOS COUNT 1: expected 1 result, got %d", len(multiRaw.Replies))
	}

	// LPOS l x -> not found
	result = testDB.Exec(nil, utils.ToCmdLine("LPOS", "l", "x"))
	if string(result.ToBytes()) != "$-1\r\n" {
		t.Fatalf("LPOS not found: %q", result.ToBytes())
	}

	// LPOS on non-existent key
	result = testDB.Exec(nil, utils.ToCmdLine("LPOS", "nokey", "x"))
	if string(result.ToBytes()) != "$-1\r\n" {
		t.Fatalf("LPOS non-existent key: %q", result.ToBytes())
	}
}

func TestLMPop(t *testing.T) {
	testDB.Flush()

	// Setup two lists
	testDB.Exec(nil, utils.ToCmdLine("RPUSH", "l1", "a", "b", "c"))
	testDB.Exec(nil, utils.ToCmdLine("RPUSH", "l2", "x", "y"))

	// LMPOP 2 l1 l2 LEFT COUNT 2 -> [l1, [a, b]]
	result := testDB.Exec(nil, utils.ToCmdLine("LMPOP", "2", "l1", "l2", "LEFT", "COUNT", "2"))
	multiRaw, ok := result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("LMPOP LEFT: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	if len(multiRaw.Replies) != 2 {
		t.Fatalf("LMPOP LEFT: expected 2 elements, got %d", len(multiRaw.Replies))
	}
	// First element is the key name
	keyReply, ok := multiRaw.Replies[0].(*protocol.BulkReply)
	if !ok {
		t.Fatalf("LMPOP LEFT: expected BulkReply for key, got %T", multiRaw.Replies[0])
	}
	if string(keyReply.Arg) != "l1" {
		t.Fatalf("LMPOP LEFT: expected key 'l1', got %q", string(keyReply.Arg))
	}

	// LMPOP 2 nokey1 nokey2 RIGHT COUNT 1 -> null
	result = testDB.Exec(nil, utils.ToCmdLine("LMPOP", "2", "nokey1", "nokey2", "RIGHT", "COUNT", "1"))
	if string(result.ToBytes()) != "$-1\r\n" {
		t.Fatalf("LMPOP empty: %q", result.ToBytes())
	}

	// LMPOP 1 l2 RIGHT -> [l2, [y]]
	result = testDB.Exec(nil, utils.ToCmdLine("LMPOP", "1", "l2", "RIGHT"))
	multiRaw, ok = result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("LMPOP RIGHT: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	if len(multiRaw.Replies) != 2 {
		t.Fatalf("LMPOP RIGHT: expected 2 elements, got %d", len(multiRaw.Replies))
	}
	keyReply, ok = multiRaw.Replies[0].(*protocol.BulkReply)
	if !ok {
		t.Fatalf("LMPOP RIGHT: expected BulkReply for key, got %T", multiRaw.Replies[0])
	}
	if string(keyReply.Arg) != "l2" {
		t.Fatalf("LMPOP RIGHT: expected key 'l2', got %q", string(keyReply.Arg))
	}
}

func TestSMisMember(t *testing.T) {
	testDB.Flush()

	// Setup
	testDB.Exec(nil, utils.ToCmdLine("SADD", "s", "a", "b", "c"))

	// SMISMEMBER s a x b -> [1, 0, 1]
	result := testDB.Exec(nil, utils.ToCmdLine("SMISMEMBER", "s", "a", "x", "b"))
	multiRaw, ok := result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("SMISMEMBER: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	if len(multiRaw.Replies) != 3 {
		t.Fatalf("SMISMEMBER: expected 3 results, got %d", len(multiRaw.Replies))
	}
	// Check values: a=1, x=0, b=1
	for i, expected := range []int{1, 0, 1} {
		intReply, ok := multiRaw.Replies[i].(*protocol.IntReply)
		if !ok {
			t.Fatalf("SMISMEMBER[%d]: expected IntReply, got %T", i, multiRaw.Replies[i])
		}
		if intReply.Code != int64(expected) {
			t.Fatalf("SMISMEMBER[%d]: expected %d, got %d", i, expected, intReply.Code)
		}
	}

	// SMISMEMBER on non-existent key
	result = testDB.Exec(nil, utils.ToCmdLine("SMISMEMBER", "nokey", "a"))
	multiRaw, ok = result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("SMISMEMBER nokey: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	intReply, ok := multiRaw.Replies[0].(*protocol.IntReply)
	if !ok {
		t.Fatalf("SMISMEMBER nokey[0]: expected IntReply, got %T", multiRaw.Replies[0])
	}
	if intReply.Code != 0 {
		t.Fatalf("SMISMEMBER nokey: expected 0, got %d", intReply.Code)
	}
}

func TestSInterCard(t *testing.T) {
	testDB.Flush()

	// Setup
	testDB.Exec(nil, utils.ToCmdLine("SADD", "s1", "a", "b", "c"))
	testDB.Exec(nil, utils.ToCmdLine("SADD", "s2", "b", "c", "d"))

	// SINTERCARD 2 s1 s2 -> 2 (b, c)
	result := testDB.Exec(nil, utils.ToCmdLine("SINTERCARD", "2", "s1", "s2"))
	asserts.AssertIntReply(t, result, 2)

	// SINTERCARD 2 s1 s2 LIMIT 1 -> 1
	result = testDB.Exec(nil, utils.ToCmdLine("SINTERCARD", "2", "s1", "s2", "LIMIT", "1"))
	asserts.AssertIntReply(t, result, 1)

	// SINTERCARD with empty set
	testDB.Exec(nil, utils.ToCmdLine("SADD", "s3", "z"))
	result = testDB.Exec(nil, utils.ToCmdLine("SINTERCARD", "2", "s1", "s3"))
	asserts.AssertIntReply(t, result, 0)
}

func TestZMPop(t *testing.T) {
	testDB.Flush()

	// Setup
	testDB.Exec(nil, utils.ToCmdLine("ZADD", "z1", "1", "a", "2", "b", "3", "c"))
	testDB.Exec(nil, utils.ToCmdLine("ZADD", "z2", "10", "x", "20", "y"))

	// ZMPOP 2 z1 z2 MIN COUNT 2 -> [z1, [[a, 1], [b, 2]]]
	result := testDB.Exec(nil, utils.ToCmdLine("ZMPOP", "2", "z1", "z2", "MIN", "COUNT", "2"))
	multiRaw, ok := result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("ZMPOP MIN: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	if len(multiRaw.Replies) != 2 {
		t.Fatalf("ZMPOP MIN: expected 2 top-level elements, got %d", len(multiRaw.Replies))
	}
	// First element is the key name
	keyReply, ok := multiRaw.Replies[0].(*protocol.BulkReply)
	if !ok {
		t.Fatalf("ZMPOP MIN: expected BulkReply for key, got %T", multiRaw.Replies[0])
	}
	if string(keyReply.Arg) != "z1" {
		t.Fatalf("ZMPOP MIN: expected key 'z1', got %q", string(keyReply.Arg))
	}

	// ZMPOP 2 nokey1 nokey2 MAX COUNT 1 -> null
	result = testDB.Exec(nil, utils.ToCmdLine("ZMPOP", "2", "nokey1", "nokey2", "MAX", "COUNT", "1"))
	if string(result.ToBytes()) != "$-1\r\n" {
		t.Fatalf("ZMPOP empty: %q", result.ToBytes())
	}

	// ZMPOP 1 z2 MAX -> [z2, [[y, 20]]]
	result = testDB.Exec(nil, utils.ToCmdLine("ZMPOP", "1", "z2", "MAX"))
	multiRaw, ok = result.(*protocol.MultiRawReply)
	if !ok {
		t.Fatalf("ZMPOP MAX: expected MultiRawReply, got %T (%s)", result, result.ToBytes())
	}
	if len(multiRaw.Replies) != 2 {
		t.Fatalf("ZMPOP MAX: expected 2 top-level elements, got %d", len(multiRaw.Replies))
	}
	keyReply, ok = multiRaw.Replies[0].(*protocol.BulkReply)
	if !ok {
		t.Fatalf("ZMPOP MAX: expected BulkReply for key, got %T", multiRaw.Replies[0])
	}
	if string(keyReply.Arg) != "z2" {
		t.Fatalf("ZMPOP MAX: expected key 'z2', got %q", string(keyReply.Arg))
	}
}

func TestObjectRefCount(t *testing.T) {
	testDB.Flush()

	// Non-existent key
	result := testDB.Exec(nil, utils.ToCmdLine("OBJECT", "REFCOUNT", "nokey"))
	errReply, ok := result.(protocol.ErrorReply)
	if !ok {
		t.Fatalf("OBJECT REFCOUNT nokey: expected error, got %T (%s)", result, result.ToBytes())
	}
	if errReply.Error() != "ERR no such key" {
		t.Fatalf("OBJECT REFCOUNT nokey: expected 'ERR no such key', got %q", errReply.Error())
	}

	// Existing key -> always 1
	testDB.Exec(nil, utils.ToCmdLine("SET", "mykey", "myval"))
	result = testDB.Exec(nil, utils.ToCmdLine("OBJECT", "REFCOUNT", "mykey"))
	asserts.AssertIntReply(t, result, 1)
}
