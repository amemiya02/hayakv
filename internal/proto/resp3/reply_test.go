package resp3

import (
	"math"
	"testing"
)

func TestRESP3NativeFrames(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"null", MakeNullReply().ToBytes(), "_\r\n"},
		{"true", MakeBoolReply(true).ToBytes(), "#t\r\n"},
		{"false", MakeBoolReply(false).ToBytes(), "#f\r\n"},
		{"double", MakeDoubleReply(3.14).ToBytes(), ",3.14\r\n"},
		{"double-inf", MakeDoubleReply(math.Inf(1)).ToBytes(), ",inf\r\n"},
		{"bignum", MakeBigNumberReply("1234567999999999999999999999999999999").ToBytes(),
			"(1234567999999999999999999999999999999\r\n"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
