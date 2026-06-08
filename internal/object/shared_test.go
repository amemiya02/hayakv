package object

import "testing"

func TestSharedIntegers(t *testing.T) {
	if SharedInt(42) != SharedInt(42) {
		t.Fatal("shared integers 0..9999 should be pointer-identical")
	}
	if SharedInt(10000) == SharedInt(10000) {
		t.Fatal("values >= 10000 must NOT be shared")
	}
	if SharedInt(-1) == SharedInt(-1) {
		t.Fatal("negative values must NOT be shared")
	}
}
