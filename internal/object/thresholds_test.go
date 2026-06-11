package object

import (
	"strconv"
	"strings"
	"testing"
)

func withThresholds(t *testing.T, tr EncodingThresholds) {
	t.Helper()
	SetEncodingThresholds(tr)
	t.Cleanup(func() { SetEncodingThresholds(DefaultEncodingThresholds()) })
}

func TestThresholdsDefaultsAndFallback(t *testing.T) {
	withThresholds(t, EncodingThresholds{}) // all zero -> defaults
	if got, want := Thresholds(), DefaultEncodingThresholds(); got != want {
		t.Fatalf("zero thresholds should fall back to defaults: got %+v", got)
	}
}

func TestHashEntryThresholdConfigurable(t *testing.T) {
	withThresholds(t, EncodingThresholds{HashMaxListpackEntries: 4})
	h := NewHash()
	for i := 0; i < 4; i++ {
		h.Put("f"+strconv.Itoa(i), "v")
	}
	if h.CurrentEncoding() != EncListpack {
		t.Fatalf("4 fields should stay listpack, got %v", h.CurrentEncoding())
	}
	h.Put("f4", "v")
	if h.CurrentEncoding() != EncHashtable {
		t.Fatalf("5th field should convert to hashtable, got %v", h.CurrentEncoding())
	}
}

func TestHashValueThresholdDefault(t *testing.T) {
	long := strings.Repeat("x", 65)

	h := NewHash()
	h.Put("f", long)
	if h.CurrentEncoding() != EncHashtable {
		t.Fatalf("65-byte value should convert to hashtable, got %v", h.CurrentEncoding())
	}

	h2 := NewHash()
	h2.Put(long, "v")
	if h2.CurrentEncoding() != EncHashtable {
		t.Fatalf("65-byte field should convert to hashtable, got %v", h2.CurrentEncoding())
	}

	// Updating an existing short field with a long value must also convert.
	h3 := NewHash()
	h3.Put("f", "short")
	h3.Put("f", long)
	if h3.CurrentEncoding() != EncHashtable {
		t.Fatalf("update to 65-byte value should convert to hashtable, got %v", h3.CurrentEncoding())
	}
}

func TestSetValueThresholdDefault(t *testing.T) {
	long := strings.Repeat("x", 65)

	s := NewSet()
	s.Add("a")
	s.Add(long)
	if s.CurrentEncoding() != EncHashtable {
		t.Fatalf("65-byte member should convert listpack set to hashtable, got %v", s.CurrentEncoding())
	}

	// From intset: a long non-int member skips the visible listpack state.
	s2 := NewSet()
	s2.Add("1")
	s2.Add(long)
	if s2.CurrentEncoding() != EncHashtable {
		t.Fatalf("65-byte member should convert intset set to hashtable, got %v", s2.CurrentEncoding())
	}
}

func TestSetEntryThresholdsConfigurable(t *testing.T) {
	withThresholds(t, EncodingThresholds{SetMaxIntsetEntries: 4, SetMaxListpackEntries: 4})
	s := NewSet()
	for i := 0; i < 4; i++ {
		s.Add(strconv.Itoa(i))
	}
	if s.CurrentEncoding() != EncIntset {
		t.Fatalf("4 ints should stay intset, got %v", s.CurrentEncoding())
	}
	s.Add("4")
	// 5 ints exceed both the intset and listpack limits (5 > 4).
	if s.CurrentEncoding() != EncHashtable {
		t.Fatalf("5th int should overflow to hashtable, got %v", s.CurrentEncoding())
	}
}

func TestZSetValueThresholdDefault(t *testing.T) {
	long := strings.Repeat("x", 65)
	z := NewZSet()
	z.Add(long, 1)
	if z.CurrentEncoding() != EncSkiplist {
		t.Fatalf("65-byte member should convert zset to skiplist, got %v", z.CurrentEncoding())
	}
}

func TestZSetEntryThresholdConfigurable(t *testing.T) {
	withThresholds(t, EncodingThresholds{ZSetMaxListpackEntries: 4})
	z := NewZSet()
	for i := 0; i < 4; i++ {
		z.Add("m"+strconv.Itoa(i), float64(i))
	}
	if z.CurrentEncoding() != EncListpack {
		t.Fatalf("4 members should stay listpack, got %v", z.CurrentEncoding())
	}
	z.Add("m4", 4)
	if z.CurrentEncoding() != EncSkiplist {
		t.Fatalf("5th member should convert to skiplist, got %v", z.CurrentEncoding())
	}
}

func TestListEntryThresholdConfigurable(t *testing.T) {
	withThresholds(t, EncodingThresholds{ListMaxListpackSize: 4})
	l := NewList()
	for i := 0; i < 4; i++ {
		l.Add("e" + strconv.Itoa(i))
	}
	if l.CurrentEncoding() != EncListpack {
		t.Fatalf("4 elements should stay listpack, got %v", l.CurrentEncoding())
	}
	l.Add("e4")
	if l.CurrentEncoding() != EncQuicklist {
		t.Fatalf("5th element should convert to quicklist, got %v", l.CurrentEncoding())
	}
}

func TestListLongElementStaysListpack(t *testing.T) {
	// Real Redis 8.4: list has no per-element value threshold; a 65-byte
	// element keeps the listpack encoding.
	l := NewList()
	l.Add(strings.Repeat("x", 65))
	if l.CurrentEncoding() != EncListpack {
		t.Fatalf("65-byte element should stay listpack, got %v", l.CurrentEncoding())
	}
}
