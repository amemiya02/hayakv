package rdb

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestParseListpack(t *testing.T) {
	cases := []struct {
		name string
		blob []byte
		want [][]byte
	}{
		{
			name: "empty",
			// total=7, num=0, terminator
			blob: []byte{0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF},
			want: nil,
		},
		{
			name: "single string abc",
			// total=12, num=1; elem: 0x83('10'+len3) 'a' 'b' 'c' backlen=0x04; 0xFF
			blob: []byte{0x0C, 0x00, 0x00, 0x00, 0x01, 0x00, 0x83, 'a', 'b', 'c', 0x04, 0xFF},
			want: [][]byte{[]byte("abc")},
		},
		{
			name: "small int 100",
			// total=9, num=1; elem: 0x64(7-bit uint 100) backlen=0x01; 0xFF
			blob: []byte{0x09, 0x00, 0x00, 0x00, 0x01, 0x00, 0x64, 0x01, 0xFF},
			want: [][]byte{[]byte("100")},
		},
		{
			name: "13-bit int 1000",
			// total=10; elem: 0xC3 0xE8 (110_00011 | 0xE8 => 3<<8|232=1000) backlen=0x02; 0xFF
			blob: []byte{0x0A, 0x00, 0x00, 0x00, 0x01, 0x00, 0xC3, 0xE8, 0x02, 0xFF},
			want: [][]byte{[]byte("1000")},
		},
		{
			name: "hash pair f1 v1",
			// total=15, num=2; two 6-bit strings, each backlen=0x03
			blob: []byte{0x0F, 0x00, 0x00, 0x00, 0x02, 0x00, 0x82, 'f', '1', 0x03, 0x82, 'v', '1', 0x03, 0xFF},
			want: [][]byte{[]byte("f1"), []byte("v1")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseListpack(tc.blob)
			if err != nil {
				t.Fatalf("parseListpack(%s): %v", tc.name, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestParseListpackInt24(t *testing.T) {
	// int24 value 100000: encoding=0xF2, data=100000 LE 3 bytes, backlen=0x04
	// 100000 = 0x0186A0 -> bytes A0 86 01
	// total = 6(header) + 1(enc) + 3(data) + 1(backlen) + 1(terminator) = 12
	blob := []byte{0x0C, 0x00, 0x00, 0x00, 0x01, 0x00, 0xF2, 0xA0, 0x86, 0x01, 0x04, 0xFF}
	got, err := parseListpack(blob)
	if err != nil {
		t.Fatalf("parseListpack: %v", err)
	}
	want := [][]byte{[]byte("100000")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseListpackInt64(t *testing.T) {
	// int64 value -123456789012345: encoding=0xF4, data LE 8 bytes, backlen=0x09
	var b [8]byte
	v := int64(-123456789012345)
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	blob := []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0xF4,
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		0x09, 0xFF}
	// Fix total-bytes: 6 header + 1(enc) + 8(data) + 1(backlen) + 1(terminator) = 17
	blob[0] = 0x11
	got, err := parseListpack(blob)
	if err != nil {
		t.Fatalf("parseListpack: %v", err)
	}
	want := [][]byte{[]byte("-123456789012345")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseListpackRejectsTruncated(t *testing.T) {
	// total-bytes claims 12 but blob is only 8 long
	blob := []byte{0x0C, 0x00, 0x00, 0x00, 0x01, 0x00, 0x83, 'a'}
	if _, err := parseListpack(blob); err == nil {
		t.Fatal("expected error for truncated listpack")
	}
}

func TestParseIntset(t *testing.T) {
	// encoding=2 (int16), length=3, values 1,2,3 (all little-endian)
	blob := []byte{
		0x02, 0x00, 0x00, 0x00, // encoding = 2 bytes/int
		0x03, 0x00, 0x00, 0x00, // length = 3
		0x01, 0x00, // 1
		0x02, 0x00, // 2
		0x03, 0x00, // 3
	}
	got, err := parseIntset(blob)
	if err != nil {
		t.Fatalf("parseIntset: %v", err)
	}
	want := [][]byte{[]byte("1"), []byte("2"), []byte("3")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseIntsetInt64Negative(t *testing.T) {
	// encoding=8 (int64), length=1, value -2
	blob := []byte{
		0x08, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0xFE, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // -2
	}
	got, err := parseIntset(blob)
	if err != nil {
		t.Fatalf("parseIntset: %v", err)
	}
	if len(got) != 1 || string(got[0]) != "-2" {
		t.Fatalf("got %q, want [-2]", got)
	}
}

func TestParseIntsetRejectsBadEncoding(t *testing.T) {
	blob := []byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if _, err := parseIntset(blob); err == nil {
		t.Fatal("expected error for encoding=3")
	}
}
