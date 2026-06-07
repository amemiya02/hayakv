package database

import "testing"

func TestLookupKeySpec(t *testing.T) {
	get, ok := LookupKeySpec("get")
	if !ok || get.FirstKey != 1 || get.LastKey != 1 || get.KeyStep != 1 {
		t.Fatalf("GET spec = %+v ok=%v", get, ok)
	}
	mset, ok := LookupKeySpec("MSET") // case-insensitive
	if !ok || mset.FirstKey != 1 || mset.LastKey != -1 || mset.KeyStep != 2 {
		t.Fatalf("MSET spec = %+v ok=%v", mset, ok)
	}
	mget, ok := LookupKeySpec("MGET")
	if !ok || mget.FirstKey != 1 || mget.LastKey != -1 || mget.KeyStep != 1 {
		t.Fatalf("MGET spec = %+v ok=%v", mget, ok)
	}
	if _, ok := LookupKeySpec("nosuchcmd"); ok {
		t.Fatal("unknown command should not have a key spec")
	}
}

func TestExtractKeysMGet(t *testing.T) {
	keys := ExtractKeys([][]byte{[]byte("MGET"), []byte("foo"), []byte("bar")})
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}
