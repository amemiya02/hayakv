package hll

import (
	"strconv"
	"testing"
)

func TestMurmur64ANonZero(t *testing.T) {
	if murmurHash64A([]byte("a"), 0xadc83b19) == 0 {
		t.Fatal("murmur returned 0 (impl missing)")
	}
}

func TestHLLAddCountAndHeader(t *testing.T) {
	h := New()
	if string(h.Bytes()[:4]) != "HYLL" {
		t.Fatalf("missing HYLL magic: %q", h.Bytes()[:4])
	}
	for i := 0; i < 1000; i++ {
		h.Add([]byte("elem" + strconv.Itoa(i)))
	}
	if c := h.Count(); c < 970 || c > 1030 {
		t.Fatalf("count = %d, want ~1000 (±2%%)", c)
	}
}

func TestHLLMerge(t *testing.T) {
	a := New()
	b := New()
	for i := 0; i < 500; i++ {
		a.Add([]byte("a" + strconv.Itoa(i)))
		b.Add([]byte("b" + strconv.Itoa(i)))
	}
	a.Merge(b)
	c := a.Count()
	if c < 970 || c > 1030 {
		t.Fatalf("merged count = %d, want ~1000", c)
	}
}
