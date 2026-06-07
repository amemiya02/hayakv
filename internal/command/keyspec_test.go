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
	if _, ok := LookupKeySpec("nosuchcmd"); ok {
		t.Fatal("unknown command should not have a key spec")
	}
}
