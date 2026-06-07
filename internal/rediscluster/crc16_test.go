package rediscluster

import "testing"

func TestKey2SlotKnownValues(t *testing.T) {
	// Values verified against real redis 8 CLUSTER KEYSLOT.
	cases := []struct {
		key  string
		slot uint16
	}{
		{"foo", 12182},
		{"bar", 5061},
		{"123456789", 12739},
		{"", 0},
		{"key:1", 6657},
		{"key:2", 10850},
		{"key:3", 14915},
	}
	for _, c := range cases {
		if got := Key2Slot(c.key); got != c.slot {
			t.Fatalf("Key2Slot(%q) = %d, want %d", c.key, got, c.slot)
		}
	}
}

func TestKey2SlotHashTags(t *testing.T) {
	// {user1000}.following and user1000 must hash identically (only the tag hashes).
	if a, b := Key2Slot("{user1000}.following"), Key2Slot("user1000"); a != b {
		t.Fatalf("hash-tag mismatch: {user1000}.following=%d user1000=%d", a, b)
	}
	if a, b := Key2Slot("{user1000}.followers"), Key2Slot("{user1000}.following"); a != b {
		t.Fatalf("two tagged keys with same tag must match: %d vs %d", a, b)
	}
	// foo{}{bar} -> no non-empty tag before first non-empty close, so hash WHOLE key.
	if a, b := Key2Slot("foo{}{bar}"), Key2Slot("foo{}{bar}"); a != b {
		t.Fatalf("determinism broken")
	}
	if got, want := Key2Slot("foo{}{bar}"), crc16([]byte("foo{}{bar}"))%slotCount; got != want {
		t.Fatalf("foo{}{bar} should hash whole key: got %d want %d", got, want)
	}
	// foo{{bar}}zap -> first { ... first } => tag is "{bar" (content between first { and next }).
	if got, want := Key2Slot("foo{{bar}}zap"), crc16([]byte("{bar"))%slotCount; got != want {
		t.Fatalf("foo{{bar}}zap tag should be \"{bar\": got %d want %d", got, want)
	}
	// {} -> empty tag => hash whole key "{}".
	if got, want := Key2Slot("{}"), crc16([]byte("{}"))%slotCount; got != want {
		t.Fatalf("{} should hash whole key: got %d want %d", got, want)
	}
	// {}{} -> first {...} is empty => hash whole key.
	if got, want := Key2Slot("{}{}"), crc16([]byte("{}{}"))%slotCount; got != want {
		t.Fatalf("{}{} should hash whole key: got %d want %d", got, want)
	}
}
