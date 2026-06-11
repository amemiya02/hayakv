package rdb

import (
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

func TestParseListpackRejectsTruncated(t *testing.T) {
	// total-bytes claims 12 but blob is only 8 long
	blob := []byte{0x0C, 0x00, 0x00, 0x00, 0x01, 0x00, 0x83, 'a'}
	if _, err := parseListpack(blob); err == nil {
		t.Fatal("expected error for truncated listpack")
	}
}
