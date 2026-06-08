package digest

import (
	"testing"
)

func TestValueDigestLength(t *testing.T) {
	d := ValueDigest([]byte("hello"))
	if len(d) != 16 {
		t.Fatalf("expected 16-char hex digest, got %d chars: %q", len(d), d)
	}
}

func TestValueDigestStable(t *testing.T) {
	d1 := ValueDigest([]byte("hello"))
	d2 := ValueDigest([]byte("hello"))
	if d1 != d2 {
		t.Fatalf("digest not stable: %q != %q", d1, d2)
	}
}

func TestValueDigestDifferent(t *testing.T) {
	d1 := ValueDigest([]byte("hello"))
	d2 := ValueDigest([]byte("world"))
	if d1 == d2 {
		t.Fatalf("different inputs should produce different digests: %q", d1)
	}
}

func TestFromHex(t *testing.T) {
	b := FromHex("deadbeef")
	if len(b) != 4 {
		t.Fatalf("expected 4 bytes, got %d", len(b))
	}
	if FromHex("zzzz") != nil {
		t.Fatal("invalid hex should return nil")
	}
}
