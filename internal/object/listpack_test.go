package object

import (
	"testing"
)

func TestEncodeStrElem(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"simple", "hello"},
		{"unicode", "你好世界"},
		{"special chars", "!@#$%^&*()"},
		{"long string", "this is a longer string for testing purposes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeStrElem(tt.input)
			if len(encoded) == 0 {
				t.Fatal("encoded element is empty")
			}

			// Verify tag
			if encoded[0] != elemStr {
				t.Fatalf("expected tag %x, got %x", elemStr, encoded[0])
			}

			// Decode and verify
			decoded, consumed, err := decodeElem(encoded)
			if err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if consumed != len(encoded) {
				t.Fatalf("consumed %d bytes, expected %d", consumed, len(encoded))
			}

			decodedStr, ok := decoded.(string)
			if !ok {
				t.Fatalf("decoded type is %T, expected string", decoded)
			}
			if decodedStr != tt.input {
				t.Fatalf("decoded %q, expected %q", decodedStr, tt.input)
			}
		})
	}
}

func TestEncodeIntElem(t *testing.T) {
	tests := []struct {
		name  string
		input int64
	}{
		{"zero", 0},
		{"positive", 42},
		{"negative", -42},
		{"max int", 9223372036854775807},
		{"min int", -9223372036854775808},
		{"small positive", 1},
		{"small negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeIntElem(tt.input)
			if len(encoded) == 0 {
				t.Fatal("encoded element is empty")
			}

			// Verify tag
			if encoded[0] != elemInt {
				t.Fatalf("expected tag %x, got %x", elemInt, encoded[0])
			}

			// Decode and verify
			decoded, consumed, err := decodeElem(encoded)
			if err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if consumed != len(encoded) {
				t.Fatalf("consumed %d bytes, expected %d", consumed, len(encoded))
			}

			decodedInt, ok := decoded.(int64)
			if !ok {
				t.Fatalf("decoded type is %T, expected int64", decoded)
			}
			if decodedInt != tt.input {
				t.Fatalf("decoded %d, expected %d", decodedInt, tt.input)
			}
		})
	}
}

func TestDecodeElemInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"unknown tag", []byte{0xFF}},
		{"truncated string", []byte{elemStr, 0x05}}, // length 5 but no data
		{"truncated int", []byte{elemInt}},          // tag only
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, consumed, _ := decodeElem(tt.input)
			if decoded != nil || consumed != 0 {
				t.Fatalf("expected nil decode for invalid input, got %v (consumed %d)", decoded, consumed)
			}
		})
	}
}

func TestIsInt(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
		value    int64
	}{
		{"0", true, 0},
		{"42", true, 42},
		{"-42", true, -42},
		{"9223372036854775807", true, 9223372036854775807},
		{"-9223372036854775807", true, -9223372036854775807},
		{"007", false, 0},                  // leading zeros
		{"", false, 0},                     // empty
		{"abc", false, 0},                  // not a number
		{"12.34", false, 0},                // float
		{" 42 ", false, 0},                 // spaces
		{"042", false, 0},                  // leading zero
		{"9223372036854775808", false, 0},  // overflow
		{"+42", false, 0},                  // Redis doesn't accept + prefix
		{"-9223372036854775808", false, 0}, // overflow
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, ok := isInt(tt.input)
			if ok != tt.expected {
				t.Fatalf("isInt(%q) = %v, want %v", tt.input, ok, tt.expected)
			}
			if ok && v != tt.value {
				t.Fatalf("isInt(%q) = %d, want %d", tt.input, v, tt.value)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Test that encoding and decoding preserves values
	strs := []string{"", "hello", "world", "123", "-456", "你好"}
	for _, s := range strs {
		encoded := encodeStrElem(s)
		decoded, _, _ := decodeElem(encoded)
		if decoded.(string) != s {
			t.Fatalf("round-trip failed for %q", s)
		}
	}

	ints := []int64{0, 1, -1, 42, -42, 9223372036854775807, -9223372036854775808}
	for _, v := range ints {
		encoded := encodeIntElem(v)
		decoded, _, _ := decodeElem(encoded)
		if decoded.(int64) != v {
			t.Fatalf("round-trip failed for %d", v)
		}
	}
}

func BenchmarkEncodeStrElem(b *testing.B) {
	for i := 0; i < b.N; i++ {
		encodeStrElem("hello world")
	}
}

func BenchmarkEncodeIntElem(b *testing.B) {
	for i := 0; i < b.N; i++ {
		encodeIntElem(42)
	}
}

func BenchmarkDecodeElem(b *testing.B) {
	encoded := encodeStrElem("hello world")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeElem(encoded)
	}
}

func TestListpackNew(t *testing.T) {
	lp := NewListpack()
	if lp.Len() != 0 {
		t.Fatalf("new listpack should have length 0, got %d", lp.Len())
	}
	if len(lp.Bytes()) != 0 {
		t.Fatalf("new listpack should have empty bytes, got %d bytes", len(lp.Bytes()))
	}
}

func TestListpackAppendStr(t *testing.T) {
	lp := NewListpack()
	lp.AppendStr("hello")
	lp.AppendStr("world")

	if lp.Len() != 2 {
		t.Fatalf("expected length 2, got %d", lp.Len())
	}

	// Verify elements
	val, ok := lp.Get(0)
	if !ok || val.(string) != "hello" {
		t.Fatalf("expected 'hello' at index 0, got %v", val)
	}
	val, ok = lp.Get(1)
	if !ok || val.(string) != "world" {
		t.Fatalf("expected 'world' at index 1, got %v", val)
	}
}

func TestListpackAppendInt(t *testing.T) {
	lp := NewListpack()
	lp.AppendInt(0)
	lp.AppendInt(42)
	lp.AppendInt(-42)

	if lp.Len() != 3 {
		t.Fatalf("expected length 3, got %d", lp.Len())
	}

	// Verify elements
	val, ok := lp.Get(0)
	if !ok || val.(int64) != 0 {
		t.Fatalf("expected 0 at index 0, got %v", val)
	}
	val, ok = lp.Get(1)
	if !ok || val.(int64) != 42 {
		t.Fatalf("expected 42 at index 1, got %v", val)
	}
	val, ok = lp.Get(2)
	if !ok || val.(int64) != -42 {
		t.Fatalf("expected -42 at index 2, got %v", val)
	}
}

func TestListpackMixed(t *testing.T) {
	lp := NewListpack()
	lp.AppendStr("hello")
	lp.AppendInt(42)
	lp.AppendStr("world")
	lp.AppendInt(-1)

	if lp.Len() != 4 {
		t.Fatalf("expected length 4, got %d", lp.Len())
	}

	// Verify mixed elements
	val, ok := lp.Get(0)
	if !ok || val.(string) != "hello" {
		t.Fatalf("expected 'hello' at index 0, got %v", val)
	}
	val, ok = lp.Get(1)
	if !ok || val.(int64) != 42 {
		t.Fatalf("expected 42 at index 1, got %v", val)
	}
	val, ok = lp.Get(2)
	if !ok || val.(string) != "world" {
		t.Fatalf("expected 'world' at index 2, got %v", val)
	}
	val, ok = lp.Get(3)
	if !ok || val.(int64) != -1 {
		t.Fatalf("expected -1 at index 3, got %v", val)
	}
}

func TestListpackGetOutOfBounds(t *testing.T) {
	lp := NewListpack()
	lp.AppendStr("hello")

	// Negative index
	_, ok := lp.Get(-1)
	if ok {
		t.Fatal("expected false for negative index")
	}

	// Index out of range
	_, ok = lp.Get(1)
	if ok {
		t.Fatal("expected false for out of range index")
	}
}

func TestListpackRange(t *testing.T) {
	lp := NewListpack()
	lp.AppendStr("a")
	lp.AppendStr("b")
	lp.AppendStr("c")

	var collected []interface{}
	lp.Range(func(i int, val interface{}) bool {
		collected = append(collected, val)
		return true
	})

	if len(collected) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(collected))
	}
	if collected[0].(string) != "a" || collected[1].(string) != "b" || collected[2].(string) != "c" {
		t.Fatalf("unexpected elements: %v", collected)
	}
}

func TestListpackRangeEarlyStop(t *testing.T) {
	lp := NewListpack()
	lp.AppendStr("a")
	lp.AppendStr("b")
	lp.AppendStr("c")

	var collected []interface{}
	lp.Range(func(i int, val interface{}) bool {
		collected = append(collected, val)
		return i < 1 // Stop after 2 elements
	})

	if len(collected) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(collected))
	}
	if collected[0].(string) != "a" || collected[1].(string) != "b" {
		t.Fatalf("unexpected elements: %v", collected)
	}
}

func TestListpackBytes(t *testing.T) {
	lp := NewListpack()
	lp.AppendStr("hello")

	bytes := lp.Bytes()
	if len(bytes) == 0 {
		t.Fatal("expected non-empty bytes")
	}

	// Verify the bytes can be decoded
	val, _, _ := decodeElem(bytes)
	if val.(string) != "hello" {
		t.Fatalf("decoded %v, expected 'hello'", val)
	}
}

func TestListpackLargeList(t *testing.T) {
	lp := NewListpack()
	for i := 0; i < 1000; i++ {
		lp.AppendInt(int64(i))
	}

	if lp.Len() != 1000 {
		t.Fatalf("expected length 1000, got %d", lp.Len())
	}

	// Verify all elements
	for i := 0; i < 1000; i++ {
		val, ok := lp.Get(i)
		if !ok || val.(int64) != int64(i) {
			t.Fatalf("expected %d at index %d, got %v", i, i, val)
		}
	}
}
