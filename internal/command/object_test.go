package database

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func TestExecObjectEncoding(t *testing.T) {
	db := makeBasicDB()

	// Test with non-existent key
	result := execObject(db, [][]byte{[]byte("ENCODING"), []byte("key1")})
	if _, ok := result.(*protocol.NullBulkReply); !ok {
		t.Fatalf("expected NullBulkReply for non-existent key, got %T", result)
	}

	// Test with legacy entity (no Robj)
	db.PutEntity("legacy", &database.DataEntity{
		Data: []byte("value"),
	})
	result = execObject(db, [][]byte{[]byte("ENCODING"), []byte("legacy")})
	bulkReply, ok := result.(*protocol.BulkReply)
	if !ok {
		t.Fatalf("expected BulkReply, got %T", result)
	}
	if string(bulkReply.Arg) != "go-native" {
		t.Fatalf("expected 'go-native', got %q", string(bulkReply.Arg))
	}

	// Test with Robj string (int encoding)
	robj := object.MakeStringObject([]byte("42"))
	db.PutEntity("intkey", &database.DataEntity{
		Data: robj,
	})
	result = execObject(db, [][]byte{[]byte("ENCODING"), []byte("intkey")})
	bulkReply, ok = result.(*protocol.BulkReply)
	if !ok {
		t.Fatalf("expected BulkReply, got %T", result)
	}
	if string(bulkReply.Arg) != "int" {
		t.Fatalf("expected 'int', got %q", string(bulkReply.Arg))
	}

	// Test with Robj string (embstr encoding)
	robj = object.MakeStringObject([]byte("hello"))
	db.PutEntity("embstrkey", &database.DataEntity{
		Data: robj,
	})
	result = execObject(db, [][]byte{[]byte("ENCODING"), []byte("embstrkey")})
	bulkReply, ok = result.(*protocol.BulkReply)
	if !ok {
		t.Fatalf("expected BulkReply, got %T", result)
	}
	if string(bulkReply.Arg) != "embstr" {
		t.Fatalf("expected 'embstr', got %q", string(bulkReply.Arg))
	}

	// Test with Robj string (raw encoding)
	longStr := "this is a long string that exceeds 44 bytes for testing raw encoding"
	robj = object.MakeStringObject([]byte(longStr))
	db.PutEntity("rawkey", &database.DataEntity{
		Data: robj,
	})
	result = execObject(db, [][]byte{[]byte("ENCODING"), []byte("rawkey")})
	bulkReply, ok = result.(*protocol.BulkReply)
	if !ok {
		t.Fatalf("expected BulkReply, got %T", result)
	}
	if string(bulkReply.Arg) != "raw" {
		t.Fatalf("expected 'raw', got %q", string(bulkReply.Arg))
	}
}

func TestExecObjectSubCommand(t *testing.T) {
	db := makeBasicDB()

	// Test missing subcommand
	result := execObject(db, [][]byte{[]byte("key1")})
	// Should return an error (ArgNumErrReply or StandardErrReply)
	if result == nil {
		t.Fatal("expected error for missing subcommand, got nil")
	}
	// Verify it's an error reply
	if _, ok := result.(protocol.ErrorReply); !ok {
		t.Fatalf("expected error reply for missing subcommand, got %T", result)
	}

	// Test unknown subcommand
	db.PutEntity("key1", &database.DataEntity{
		Data: []byte("value"),
	})
	result = execObject(db, [][]byte{[]byte("UNKNOWN"), []byte("key1")})
	if result == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
	// Verify it's an error reply
	if _, ok := result.(protocol.ErrorReply); !ok {
		t.Fatalf("expected error reply for unknown subcommand, got %T", result)
	}
}
