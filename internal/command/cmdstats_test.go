package database

import "testing"

func TestCmdStatsRecord(t *testing.T) {
	st := newCmdStats()
	st.record("get", 120, false)
	st.record("get", 80, false)
	st.record("get", 50, true)
	s := st.snapshot()["get"]
	if s.calls != 3 || s.usec != 250 || s.failed != 1 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestLog2Bucket(t *testing.T) {
	tests := []struct {
		usec   int64
		expect int
	}{
		{0, 0},
		{1, 0},
		{2, 1},
		{4, 2},
		{8, 3},
		{1000, 9},
		{1000000, 19}, // clamped to 15
	}
	for _, tt := range tests {
		got := log2Bucket(tt.usec)
		expected := tt.expect
		if expected > 15 {
			expected = 15
		}
		if got != expected {
			t.Errorf("log2Bucket(%d) = %d, want %d", tt.usec, got, expected)
		}
	}
}

func TestErrorPrefix(t *testing.T) {
	if got := errorPrefix([]byte("-ERR unknown command\r\n")); got != "ERR" {
		t.Fatalf("errorPrefix: %q", got)
	}
	if got := errorPrefix([]byte("-WRONGTYPE Operation against a key\r\n")); got != "WRONGTYPE" {
		t.Fatalf("errorPrefix: %q", got)
	}
}
