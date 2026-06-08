package database

import (
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"testing"
)

func TestBitfield(t *testing.T) {
	testDB.Flush()
	// SET u8 at offset #0 = 255, then GET u8 #0
	testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "u8", "#0", "255"))
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "GET", "u8", "#0"))
	expected := "*1\r\n:255\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD GET after SET = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldSetGet(t *testing.T) {
	testDB.Flush()
	// SET and GET in one command
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "u8", "#0", "255", "GET", "u8", "#0"))
	expected := "*2\r\n:0\r\n:255\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD SET/GET = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldOverflowSAT(t *testing.T) {
	testDB.Flush()
	// Set to 255 (max u8), then INCRBY 10 with SAT overflow
	testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "u8", "#0", "255"))
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "OVERFLOW", "SAT", "INCRBY", "u8", "#0", "10"))
	expected := "*1\r\n:255\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD SAT overflow = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldOverflowFAIL(t *testing.T) {
	testDB.Flush()
	// Set to 255 (max u8), then INCRBY 1 with FAIL overflow
	testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "u8", "#0", "255"))
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "OVERFLOW", "FAIL", "INCRBY", "u8", "#0", "1"))
	expected := "*1\r\n$-1\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD FAIL overflow = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldOverflowWRAP(t *testing.T) {
	testDB.Flush()
	// Set to 255 (max u8), then INCRBY 1 with WRAP overflow
	testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "u8", "#0", "255"))
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "OVERFLOW", "WRAP", "INCRBY", "u8", "#0", "1"))
	expected := "*1\r\n:0\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD WRAP overflow = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldSigned(t *testing.T) {
	testDB.Flush()
	// Set i8 to -1 (all bits set = 255 unsigned), then GET
	testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "i8", "#0", "-1"))
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "GET", "i8", "#0"))
	expected := "*1\r\n:-1\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD signed GET = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldRO(t *testing.T) {
	testDB.Flush()
	testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "u8", "#0", "42"))
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD_RO", "bf", "GET", "u8", "#0"))
	expected := "*1\r\n:42\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD_RO GET = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldRONonGET(t *testing.T) {
	testDB.Flush()
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD_RO", "bf", "SET", "u8", "#0", "1"))
	errReply, ok := actual.(protocol.ErrorReply)
	if !ok {
		t.Fatalf("BITFIELD_RO SET should return error, got %q", actual.ToBytes())
	}
	if string(errReply.ToBytes()) != "-ERR BITFIELD_RO only supports the GET subcommand\r\n" {
		t.Fatalf("BITFIELD_RO SET error = %q", errReply.ToBytes())
	}
}

func TestBitfieldMissingKey(t *testing.T) {
	testDB.Flush()
	// GET from a missing key should return 0
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "miss", "GET", "u8", "#0"))
	expected := "*1\r\n:0\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD GET missing key = %q, want %q", actual.ToBytes(), expected)
	}
}

func TestBitfieldBitOffset(t *testing.T) {
	testDB.Flush()
	// Test bit-offset addressing (not #N)
	testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "SET", "u8", "0", "170"))
	actual := testDB.Exec(nil, utils.ToCmdLine("BITFIELD", "bf", "GET", "u8", "0"))
	expected := "*1\r\n:170\r\n"
	if string(actual.ToBytes()) != expected {
		t.Fatalf("BITFIELD bit offset = %q, want %q", actual.ToBytes(), expected)
	}
}
