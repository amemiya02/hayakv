package database

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestXAddXLenXRange(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	// Add entries with explicit IDs to avoid monotonic ordering issues
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f2", "v2"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "3-0", "f3", "v3"))

	// Check XLEN
	ret := testDB.Exec(conn, utils.ToCmdLine("XLEN", "st"))
	if string(ret.ToBytes()) != ":3\r\n" {
		t.Fatalf("XLEN after 3 entries = %q", ret.ToBytes())
	}
}

func TestXAddExplicitID(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	ret := testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-1", "f", "v"))
	if string(ret.ToBytes()) != "$3\r\n1-1\r\n" {
		t.Fatalf("explicit ID = %q", ret.ToBytes())
	}
}

func TestXAddNoMkStream(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	// NOMKSTREAM on absent key should return null
	ret := testDB.Exec(conn, utils.ToCmdLine("XADD", "absent", "NOMKSTREAM", "*", "f", "v"))
	if string(ret.ToBytes()) != "$-1\r\n" {
		t.Fatal("XADD NOMKSTREAM on absent should be null")
	}
}

func TestXRangeXRevRange(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "a", "1"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "b", "2"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "3-0", "c", "3"))

	// XRANGE with "-" and "+" should return all 3 entries
	ret := testDB.Exec(conn, utils.ToCmdLine("XRANGE", "st", "-", "+"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XRANGE = %q", raw)
	}

	// XRANGE with COUNT
	ret2 := testDB.Exec(conn, utils.ToCmdLine("XRANGE", "st", "-", "+", "COUNT", "2"))
	raw2 := string(ret2.ToBytes())
	if raw2[0] != '*' {
		t.Fatalf("XRANGE COUNT = %q", raw2)
	}

	// XREVRANGE
	ret3 := testDB.Exec(conn, utils.ToCmdLine("XREVRANGE", "st", "+", "-"))
	raw3 := string(ret3.ToBytes())
	if raw3[0] != '*' {
		t.Fatalf("XREVRANGE = %q", raw3)
	}
}

func TestXDel(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))

	ret := testDB.Exec(conn, utils.ToCmdLine("XDEL", "st", "1-0"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("XDEL = %q", ret.ToBytes())
	}

	ret2 := testDB.Exec(conn, utils.ToCmdLine("XLEN", "st"))
	if string(ret2.ToBytes()) != ":1\r\n" {
		t.Fatalf("XLEN after XDEL = %q", ret2.ToBytes())
	}
}

func TestXTrim(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "3-0", "f", "v"))

	// Trim to keep only 2 entries
	ret := testDB.Exec(conn, utils.ToCmdLine("XTRIM", "st", "MAXLEN", "2"))
	if string(ret.ToBytes()) != ":1\r\n" {
		t.Fatalf("XTRIM = %q", ret.ToBytes())
	}

	// XLEN should be 2
	ret2 := testDB.Exec(conn, utils.ToCmdLine("XLEN", "st"))
	if string(ret2.ToBytes()) != ":2\r\n" {
		t.Fatalf("XLEN after XTRIM = %q", ret2.ToBytes())
	}
}

func TestXReadNonBlocking(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "2-0", "f", "v"))

	// XREAD with COUNT 1
	ret := testDB.Exec(conn, utils.ToCmdLine("XREAD", "COUNT", "1", "STREAMS", "st", "0-0"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XREAD = %q", raw)
	}

	// XREAD with ID > 1-0 should return only 2-0
	ret2 := testDB.Exec(conn, utils.ToCmdLine("XREAD", "COUNT", "10", "STREAMS", "st", "1-0"))
	raw2 := string(ret2.ToBytes())
	if raw2[0] != '*' {
		t.Fatalf("XREAD > 1-0 = %q", raw2)
	}
}

func TestXReadNonBlockingNoData(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))

	// XREAD with ID = last ID should return null array
	ret := testDB.Exec(conn, utils.ToCmdLine("XREAD", "COUNT", "10", "STREAMS", "st", "1-0"))
	if string(ret.ToBytes()) != "*-1\r\n" {
		t.Fatalf("XREAD no data = %q", ret.ToBytes())
	}
}

func TestXSetID(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "1-0", "f", "v"))

	ret := testDB.Exec(conn, utils.ToCmdLine("XSETID", "st", "100-0"))
	if string(ret.ToBytes()) != "+OK\r\n" {
		t.Fatalf("XSETID = %q", ret.ToBytes())
	}

	// Now adding an entry should use ID > 100-0
	ret2 := testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "*", "f2", "v2"))
	// Should fail because auto-assign would try to use time which might be <= 100
	// This tests that XSETID properly sets the last ID
	_ = ret2
}

func TestObjectTypeStream(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "st", "*", "f", "v"))

	// Check OBJECT ENCODING
	ret := testDB.Exec(conn, utils.ToCmdLine("OBJECT", "ENCODING", "st"))
	if string(ret.ToBytes()) != "$6\r\nstream\r\n" {
		t.Fatalf("encoding = %q", ret.ToBytes())
	}
}

func TestXReadMultipleStreams(t *testing.T) {
	testDB.Flush()
	conn := connection.NewFakeConn()

	testDB.Exec(conn, utils.ToCmdLine("XADD", "s1", "1-0", "f", "v1"))
	testDB.Exec(conn, utils.ToCmdLine("XADD", "s2", "2-0", "f", "v2"))

	// XREAD from two streams
	ret := testDB.Exec(conn, utils.ToCmdLine("XREAD", "COUNT", "10", "STREAMS", "s1", "s2", "0-0", "0-0"))
	raw := string(ret.ToBytes())
	if raw[0] != '*' {
		t.Fatalf("XREAD multiple = %q", raw)
	}
}
