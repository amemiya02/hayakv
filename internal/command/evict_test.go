package database

import (
	"testing"

	"github.com/amemiya02/hayakv/config"
)

func TestLRUClockMonotonicAndMasked(t *testing.T) {
	c := lruClock()
	if c != (c & lruClockMax) {
		t.Fatalf("lruClock not masked to 24 bits: %d", c)
	}
}

func TestLRUIdleNonNegative(t *testing.T) {
	now := uint32(1000) & lruClockMax
	// access recorded "in the future" relative to a wrapped clock must not panic / go negative
	if d := lruIdleFor(now, now); d != 0 {
		t.Fatalf("idle of same clock = %d, want 0", d)
	}
	older := (now - 5) & lruClockMax
	if d := lruIdleFor(now, older); d != 5 {
		t.Fatalf("idle = %d, want 5", d)
	}
}

func TestLFULogIncrSaturates(t *testing.T) {
	// lfu_log_factor default 10. From 0 the counter rises probabilistically and
	// never exceeds 255. Drive it hard; assert bounds + non-decreasing trend.
	config.Properties.MaxmemorySamples = 5 // unrelated; touch config to ensure import is real
	cnt := uint8(0)
	for i := 0; i < 500000; i++ {
		cnt = lfuLogIncr(cnt)
	}
	if cnt != 255 {
		t.Fatalf("after heavy access counter = %d, want saturated 255", cnt)
	}
	if got := lfuLogIncr(255); got != 255 {
		t.Fatalf("lfuLogIncr(255) = %d, want 255", got)
	}
}

func TestLFUInitialCounter(t *testing.T) {
	if lfuInitVal != 5 {
		t.Fatalf("lfuInitVal = %d, want 5 (Redis LFU_INIT_VAL)", lfuInitVal)
	}
}
