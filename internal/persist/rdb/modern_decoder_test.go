package rdb

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

// rdbStr encodes b as an RDB length-prefixed raw string (valid for len < 64).
func rdbStr(b []byte) []byte {
	if len(b) >= 64 {
		panic("rdbStr test helper only handles short strings")
	}
	return append([]byte{byte(len(b))}, b...)
}

// modernFixture builds a minimal single-record RDB: "REDIS0011", SELECTDB 0,
// [typeByte] key payload, EOF, and a zero (disabled) CRC trailer.
func modernFixture(typeByte byte, key string, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteString("REDIS0011")
	b.WriteByte(opSelectDB)
	b.WriteByte(0x00) // db 0 (len6 zero)
	b.WriteByte(typeByte)
	b.Write(rdbStr([]byte(key)))
	b.Write(payload)
	b.WriteByte(opEOF)
	b.Write(make([]byte, 8)) // zero crc -> verifyCRC accepts
	return b.Bytes()
}

func decodeOne(t *testing.T, data []byte) Entry {
	t.Helper()
	dec := NewDecoder(bytes.NewReader(data))
	var got []Entry
	if err := dec.Parse(func(e Entry) bool { got = append(got, e); return true }); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	return got[0]
}

func TestDecodeSetIntset(t *testing.T) {
	intset := []byte{0x02, 0, 0, 0, 0x03, 0, 0, 0, 0x01, 0x00, 0x02, 0x00, 0x03, 0x00}
	e := decodeOne(t, modernFixture(typeSetIntset, "s", rdbStr(intset)))
	if e.Type != typeSet {
		t.Fatalf("Type = %d, want typeSet(%d)", e.Type, typeSet)
	}
	want := [][]byte{[]byte("1"), []byte("2"), []byte("3")}
	if !reflect.DeepEqual(e.SetVal, want) {
		t.Fatalf("SetVal = %q, want %q", e.SetVal, want)
	}
}

func TestDecodeHashListpack(t *testing.T) {
	lp := []byte{0x0F, 0, 0, 0, 0x02, 0x00, 0x82, 'f', '1', 0x03, 0x82, 'v', '1', 0x03, 0xFF}
	e := decodeOne(t, modernFixture(typeHashListpack, "h", rdbStr(lp)))
	if e.Type != typeHash {
		t.Fatalf("Type = %d, want typeHash(%d)", e.Type, typeHash)
	}
	if !bytes.Equal(e.HashVal["f1"], []byte("v1")) {
		t.Fatalf("HashVal = %v", e.HashVal)
	}
}

func TestDecodeZSet2(t *testing.T) {
	var payload bytes.Buffer
	payload.WriteByte(0x01) // count = 1 (len6)
	payload.Write(rdbStr([]byte("m")))
	var d [8]byte
	binary.LittleEndian.PutUint64(d[:], math.Float64bits(2.5))
	payload.Write(d[:])
	e := decodeOne(t, modernFixture(typeZSet2, "z", payload.Bytes()))
	if e.Type != typeZSet {
		t.Fatalf("Type = %d, want typeZSet(%d)", e.Type, typeZSet)
	}
	if len(e.ZSetVal) != 1 || string(e.ZSetVal[0].Member) != "m" || e.ZSetVal[0].Score != 2.5 {
		t.Fatalf("ZSetVal = %+v", e.ZSetVal)
	}
}

func TestDecodeZSetListpack(t *testing.T) {
	// listpack ["m", "2.5"]: m -> 0x81 'm' 0x02 ; "2.5" -> 0x83 '2' '.' '5' 0x04
	lp := []byte{0x0F, 0, 0, 0, 0x02, 0x00, 0x81, 'm', 0x02, 0x83, '2', '.', '5', 0x04, 0xFF}
	e := decodeOne(t, modernFixture(typeZSetListpack, "z", rdbStr(lp)))
	if e.Type != typeZSet {
		t.Fatalf("Type = %d, want typeZSet(%d)", e.Type, typeZSet)
	}
	if len(e.ZSetVal) != 1 || string(e.ZSetVal[0].Member) != "m" || e.ZSetVal[0].Score != 2.5 {
		t.Fatalf("ZSetVal = %+v", e.ZSetVal)
	}
}

func TestDecodeQuicklist2Packed(t *testing.T) {
	// one PACKED node containing listpack ["a","b"]
	lp := []byte{0x0D, 0, 0, 0, 0x02, 0x00, 0x81, 'a', 0x02, 0x81, 'b', 0x02, 0xFF}
	var payload bytes.Buffer
	payload.WriteByte(0x01) // nodeCount = 1
	payload.WriteByte(0x02) // container = PACKED
	payload.Write(rdbStr(lp))
	e := decodeOne(t, modernFixture(typeListQuicklist2, "l", payload.Bytes()))
	if e.Type != typeList {
		t.Fatalf("Type = %d, want typeList(%d)", e.Type, typeList)
	}
	want := [][]byte{[]byte("a"), []byte("b")}
	if !reflect.DeepEqual(e.ListVal, want) {
		t.Fatalf("ListVal = %q, want %q", e.ListVal, want)
	}
}

func TestDecodeQuicklist2PlainAndPacked(t *testing.T) {
	// node 1: PLAIN "big" ; node 2: PACKED listpack ["c"]
	lpC := []byte{0x0A, 0, 0, 0, 0x01, 0x00, 0x81, 'c', 0x02, 0xFF}
	var payload bytes.Buffer
	payload.WriteByte(0x02) // nodeCount = 2
	payload.WriteByte(0x01) // container = PLAIN
	payload.Write(rdbStr([]byte("big")))
	payload.WriteByte(0x02) // container = PACKED
	payload.Write(rdbStr(lpC))
	e := decodeOne(t, modernFixture(typeListQuicklist2, "l", payload.Bytes()))
	want := [][]byte{[]byte("big"), []byte("c")}
	if !reflect.DeepEqual(e.ListVal, want) {
		t.Fatalf("ListVal = %q, want %q", e.ListVal, want)
	}
}

func TestDecodeSetListpack(t *testing.T) {
	// listpack ["x","y"]
	lp := []byte{0x0D, 0, 0, 0, 0x02, 0x00, 0x81, 'x', 0x02, 0x81, 'y', 0x02, 0xFF}
	e := decodeOne(t, modernFixture(typeSetListpack, "s", rdbStr(lp)))
	if e.Type != typeSet {
		t.Fatalf("Type = %d, want typeSet(%d)", e.Type, typeSet)
	}
	want := [][]byte{[]byte("x"), []byte("y")}
	if !reflect.DeepEqual(e.SetVal, want) {
		t.Fatalf("SetVal = %q, want %q", e.SetVal, want)
	}
}
