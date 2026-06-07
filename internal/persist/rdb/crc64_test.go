package rdb

import "testing"

func TestCRC64RedisVector(t *testing.T) {
	// Redis's own crc64 test: crc64(0, "123456789") == 0xe9c6d914c4b8d9ca
	got := crc64Update(0, []byte("123456789"))
	if got != 0xe9c6d914c4b8d9ca {
		t.Fatalf("crc64 vector mismatch: got %#016x want 0xe9c6d914c4b8d9ca", got)
	}
}

func TestCRC64Incremental(t *testing.T) {
	// Updating in chunks must equal a one-shot update.
	full := crc64Update(0, []byte("hayakv-rocks"))
	part := crc64Update(0, []byte("hayakv-"))
	part = crc64Update(part, []byte("rocks"))
	if full != part {
		t.Fatalf("incremental crc64 mismatch: %#x != %#x", full, part)
	}
}
