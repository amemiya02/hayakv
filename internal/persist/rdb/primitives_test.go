package rdb

import (
	"bytes"
	"testing"
)

func TestLenRoundTrip(t *testing.T) {
	for _, n := range []uint64{0, 1, 63, 64, 16383, 16384, 1 << 20, 1 << 33} {
		var buf bytes.Buffer
		w := newWriter(&buf)
		if err := w.writeLen(n); err != nil {
			t.Fatalf("writeLen(%d): %v", n, err)
		}
		r := newReader(bytes.NewReader(buf.Bytes()))
		got, special, err := r.readLen()
		if err != nil || special {
			t.Fatalf("readLen(%d): got=%d special=%v err=%v", n, got, special, err)
		}
		if got != n {
			t.Fatalf("len round-trip %d != %d", got, n)
		}
	}
}

func TestStringRoundTrip(t *testing.T) {
	cases := [][]byte{[]byte(""), []byte("hello"), []byte("12345"), bytes.Repeat([]byte("x"), 70000)}
	for _, c := range cases {
		var buf bytes.Buffer
		w := newWriter(&buf)
		if err := w.writeString(c); err != nil {
			t.Fatalf("writeString(%q): %v", c, err)
		}
		r := newReader(bytes.NewReader(buf.Bytes()))
		got, err := r.readString()
		if err != nil {
			t.Fatalf("readString(%q): %v", c, err)
		}
		if !bytes.Equal(got, c) {
			t.Fatalf("string round-trip mismatch len %d", len(c))
		}
	}
}

func TestIntStringEncoding(t *testing.T) {
	// "12345" must be stored via the special int encoding, not raw bytes.
	var buf bytes.Buffer
	w := newWriter(&buf)
	if err := w.writeString([]byte("12345")); err != nil {
		t.Fatalf("writeString: %v", err)
	}
	b := buf.Bytes()
	if b[0]>>6 != 3 { // top two bits == 11 => special
		t.Fatalf("expected special-encoded int, first byte = %#x", b[0])
	}
}
