package object

import "sync/atomic"

// EncodingThresholds mirrors the Redis *-max-listpack-* configuration keys
// that decide when a compact encoding converts to its full structure.
type EncodingThresholds struct {
	HashMaxListpackEntries int
	HashMaxListpackValue   int
	SetMaxIntsetEntries    int
	SetMaxListpackEntries  int
	SetMaxListpackValue    int
	ZSetMaxListpackEntries int
	ZSetMaxListpackValue   int
	ListMaxListpackSize    int
}

// DefaultEncodingThresholds returns the real Redis 8.x defaults.
func DefaultEncodingThresholds() EncodingThresholds {
	return EncodingThresholds{
		HashMaxListpackEntries: 128,
		HashMaxListpackValue:   64,
		SetMaxIntsetEntries:    512,
		SetMaxListpackEntries:  128,
		SetMaxListpackValue:    64,
		ZSetMaxListpackEntries: 128,
		ZSetMaxListpackValue:   64,
		ListMaxListpackSize:    128,
	}
}

var encodingThresholds atomic.Pointer[EncodingThresholds]

func init() {
	def := DefaultEncodingThresholds()
	encodingThresholds.Store(&def)
}

// SetEncodingThresholds installs new conversion thresholds. Non-positive
// fields fall back to the Redis defaults so an absent config key keeps
// faithful behavior.
func SetEncodingThresholds(t EncodingThresholds) {
	def := DefaultEncodingThresholds()
	if t.HashMaxListpackEntries <= 0 {
		t.HashMaxListpackEntries = def.HashMaxListpackEntries
	}
	if t.HashMaxListpackValue <= 0 {
		t.HashMaxListpackValue = def.HashMaxListpackValue
	}
	if t.SetMaxIntsetEntries <= 0 {
		t.SetMaxIntsetEntries = def.SetMaxIntsetEntries
	}
	if t.SetMaxListpackEntries <= 0 {
		t.SetMaxListpackEntries = def.SetMaxListpackEntries
	}
	if t.SetMaxListpackValue <= 0 {
		t.SetMaxListpackValue = def.SetMaxListpackValue
	}
	if t.ZSetMaxListpackEntries <= 0 {
		t.ZSetMaxListpackEntries = def.ZSetMaxListpackEntries
	}
	if t.ZSetMaxListpackValue <= 0 {
		t.ZSetMaxListpackValue = def.ZSetMaxListpackValue
	}
	if t.ListMaxListpackSize <= 0 {
		t.ListMaxListpackSize = def.ListMaxListpackSize
	}
	encodingThresholds.Store(&t)
}

// Thresholds returns the current encoding conversion thresholds.
func Thresholds() EncodingThresholds {
	return *encodingThresholds.Load()
}

// elemLen reports the byte length of a listpack element candidate. Integers
// render to at most 20 digits, far below any value threshold, so they never
// force a conversion.
func elemLen(value interface{}) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case []byte:
		return len(v)
	default:
		return 0
	}
}
