package resp3

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func TestEncodeRESP3TranslatesNulls(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"null-bulk", protocol.MakeNullBulkReply().ToBytes(), "_\r\n"},
		{"empty-multibulk", protocol.MakeEmptyMultiBulkReply().ToBytes(), "*0\r\n"},
		{"status", protocol.MakeStatusReply("OK").ToBytes(), "+OK\r\n"},
		{"int", protocol.MakeIntReply(7).ToBytes(), ":7\r\n"},
		{"bulk", protocol.MakeBulkReply([]byte("hi")).ToBytes(), "$2\r\nhi\r\n"},
	}
	for _, c := range cases {
		if got := EncodeRESP3FromRESP2(c.in); string(got) != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
