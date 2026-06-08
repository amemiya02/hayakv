package protocol

import "testing"

func TestInternedNullBulk(t *testing.T) {
	a := MakeNullBulkReply()
	b := MakeNullBulkReply()
	if a != b {
		t.Fatal("MakeNullBulkReply should return singleton")
	}
	if string(a.ToBytes()) != "$-1\r\n" {
		t.Fatalf("unexpected bytes: %q", a.ToBytes())
	}
}

func TestInternedEmptyMultiBulk(t *testing.T) {
	a := MakeEmptyMultiBulkReply()
	b := MakeEmptyMultiBulkReply()
	if a != b {
		t.Fatal("MakeEmptyMultiBulkReply should return singleton")
	}
	if string(a.ToBytes()) != "*0\r\n" {
		t.Fatalf("unexpected bytes: %q", a.ToBytes())
	}
}

func TestInternedIntReply(t *testing.T) {
	if MakeIntReply(0) != MakeIntReply(0) {
		t.Fatal("MakeIntReply(0) should return singleton")
	}
	if MakeIntReply(1) != MakeIntReply(1) {
		t.Fatal("MakeIntReply(1) should return singleton")
	}
	if MakeIntReply(42) == MakeIntReply(42) {
		t.Fatal("MakeIntReply(42) must NOT be shared")
	}
}
