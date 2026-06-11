package rdb

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

func encodeFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	_ = enc.WriteAux("redis-ver", "8.0.0")
	_ = enc.WriteSelectDB(0)
	_ = enc.WriteResizeDB(5, 1)
	_ = enc.WriteStringEntry([]byte("s"), []byte("hello"), 0)
	_ = enc.WriteStringEntry([]byte("e"), []byte("v"), 1700000000000)
	_ = enc.WriteListEntry([]byte("l"), [][]byte{[]byte("a"), []byte("b")}, 0)
	_ = enc.WriteSetEntry([]byte("se"), [][]byte{[]byte("x"), []byte("y")}, 0)
	_ = enc.WriteHashEntry([]byte("h"), map[string][]byte{"f": []byte("1")}, 0)
	_ = enc.WriteZSetEntry([]byte("z"), []ZSetMember{{Member: []byte("m"), Score: 1.5}}, 0)
	_ = enc.WriteSelectDB(1)
	_ = enc.WriteStringEntry([]byte("d1k"), []byte("d1v"), 0)
	if err := enc.WriteEnd(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecoderRoundTrip(t *testing.T) {
	data := encodeFixture(t)
	dec := NewDecoder(bytes.NewReader(data))
	var got []Entry
	err := dec.Parse(func(e Entry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("got %d entries, want 7", len(got))
	}
	// spot-check a few decoded shapes
	byKey := map[string]Entry{}
	for _, e := range got {
		byKey[string(e.Key)] = e
	}
	if e := byKey["s"]; e.Type != typeString || !bytes.Equal(e.StringVal, []byte("hello")) || e.DBIndex != 0 {
		t.Fatalf("string entry wrong: %+v", e)
	}
	if e := byKey["e"]; e.ExpireMS != 1700000000000 {
		t.Fatalf("expire not decoded: %+v", e)
	}
	if e := byKey["l"]; e.Type != typeList || !reflect.DeepEqual(e.ListVal, [][]byte{[]byte("a"), []byte("b")}) {
		t.Fatalf("list entry wrong: %+v", e)
	}
	if e := byKey["h"]; e.Type != typeHash || !bytes.Equal(e.HashVal["f"], []byte("1")) {
		t.Fatalf("hash entry wrong: %+v", e)
	}
	if e := byKey["z"]; e.Type != typeZSet || len(e.ZSetVal) != 1 || e.ZSetVal[0].Score != 1.5 {
		t.Fatalf("zset entry wrong: %+v", e)
	}
	if e := byKey["d1k"]; e.DBIndex != 1 {
		t.Fatalf("db index not tracked: %+v", e)
	}
}

func TestDecoderRejectsBadCRC(t *testing.T) {
	data := encodeFixture(t)
	data[len(data)-1] ^= 0xFF // corrupt the trailer
	dec := NewDecoder(bytes.NewReader(data))
	err := dec.Parse(func(Entry) bool { return true })
	if err == nil {
		t.Fatal("expected CRC mismatch error")
	}
}

func TestReadDouble(t *testing.T) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(3.5))
	d := NewDecoder(bytes.NewReader(b[:]))
	got, err := d.readDouble()
	if err != nil {
		t.Fatalf("readDouble: %v", err)
	}
	if got != 3.5 {
		t.Fatalf("got %v, want 3.5", got)
	}
}
