package object

import (
	"testing"
)

func TestRobjTypeName(t *testing.T) {
	tests := []struct {
		typ      ObjType
		expected string
	}{
		{TypeString, "string"},
		{TypeList, "list"},
		{TypeSet, "set"},
		{TypeHash, "hash"},
		{TypeZSet, "zset"},
		{ObjType(99), "unknown"},
	}

	for _, tt := range tests {
		r := NewRobj(tt.typ, EncRaw, nil)
		if r.TypeName() != tt.expected {
			t.Fatalf("expected %q, got %q", tt.expected, r.TypeName())
		}
	}
}

func TestRobjEncodingName(t *testing.T) {
	tests := []struct {
		enc      Encoding
		expected string
	}{
		{EncInt, "int"},
		{EncEmbstr, "embstr"},
		{EncRaw, "raw"},
		{EncListpack, "listpack"},
		{EncQuicklist, "quicklist"},
		{EncIntset, "intset"},
		{EncHashtable, "hashtable"},
		{EncSkiplist, "skiplist"},
		{Encoding(99), "unknown"},
	}

	for _, tt := range tests {
		r := NewRobj(TypeString, tt.enc, nil)
		if r.EncodingName() != tt.expected {
			t.Fatalf("expected %q, got %q", tt.expected, r.EncodingName())
		}
	}
}

func TestMakeStringObjectInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0", 0},
		{"42", 42},
		{"-42", -42},
		{"9223372036854775807", 9223372036854775807},
	}

	for _, tt := range tests {
		r := MakeStringObject([]byte(tt.input))
		if r.Type != TypeString {
			t.Fatalf("expected TypeString, got %d", r.Type)
		}
		if r.Encoding != EncInt {
			t.Fatalf("expected EncInt for %q, got %d", tt.input, r.Encoding)
		}
		v, ok := r.GetStringAsInt()
		if !ok {
			t.Fatalf("expected int value for %q", tt.input)
		}
		if v != tt.expected {
			t.Fatalf("expected %d, got %d", tt.expected, v)
		}
	}
}

func TestMakeStringObjectEmbstr(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"hello"},
		{"this is a test string for embstr"}, // 33 bytes
	}

	for _, tt := range tests {
		r := MakeStringObject([]byte(tt.input))
		if r.Type != TypeString {
			t.Fatalf("expected TypeString, got %d", r.Type)
		}
		if r.Encoding != EncEmbstr {
			t.Fatalf("expected EncEmbstr for %q (len=%d), got %d", tt.input, len(tt.input), r.Encoding)
		}
		bytes := r.GetStringBytes()
		if string(bytes) != tt.input {
			t.Fatalf("expected %q, got %q", tt.input, string(bytes))
		}
	}
}

func TestMakeStringObjectRaw(t *testing.T) {
	// 45 bytes - should be raw
	input := "this is a test string that is exactly 45 bytes"
	if len(input) <= 44 {
		t.Fatalf("test string should be >44 bytes, got %d", len(input))
	}

	r := MakeStringObject([]byte(input))
	if r.Type != TypeString {
		t.Fatalf("expected TypeString, got %d", r.Type)
	}
	if r.Encoding != EncRaw {
		t.Fatalf("expected EncRaw for string of len %d, got %d", len(input), r.Encoding)
	}
	bytes := r.GetStringBytes()
	if string(bytes) != input {
		t.Fatalf("expected %q, got %q", input, string(bytes))
	}
}

func TestMakeStringObjectNonInt(t *testing.T) {
	// These look like numbers but aren't valid ints
	tests := []string{
		"007",     // leading zeros
		"042",     // leading zeros
		"12.34",   // float
		"abc",     // not a number
		"+42",     // + prefix
		" 42 ",    // spaces
	}

	for _, input := range tests {
		r := MakeStringObject([]byte(input))
		if r.Type != TypeString {
			t.Fatalf("expected TypeString for %q, got %d", input, r.Type)
		}
		if r.Encoding == EncInt {
			t.Fatalf("should not be EncInt for %q", input)
		}
		bytes := r.GetStringBytes()
		if string(bytes) != input {
			t.Fatalf("expected %q, got %q", input, string(bytes))
		}
	}
}

func TestRobjValue(t *testing.T) {
	r := NewRobj(TypeString, EncRaw, []byte("hello"))
	if r.Value() == nil {
		t.Fatal("expected non-nil value")
	}
	bytes, ok := r.Value().([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", r.Value())
	}
	if string(bytes) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(bytes))
	}
}

func TestRobjGetStringAsInt(t *testing.T) {
	// Int encoding
	r := NewRobj(TypeString, EncInt, int64(42))
	v, ok := r.GetStringAsInt()
	if !ok {
		t.Fatal("expected true for int encoding")
	}
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}

	// Non-int encoding
	r = NewRobj(TypeString, EncRaw, []byte("42"))
	_, ok = r.GetStringAsInt()
	if ok {
		t.Fatal("expected false for non-int encoding")
	}
}
