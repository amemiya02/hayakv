package resp2

import (
	"bytes"
	"testing"

	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func TestCodecDecodeOneAndEncodeRESP2(t *testing.T) {
	codec := Codec{}
	reply, err := codec.DecodeOne([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("DecodeOne returned error: %v", err)
	}

	encoded := codec.Encode(reply, iface.RESP2)
	if !bytes.Equal(encoded, []byte("*1\r\n$4\r\nPING\r\n")) {
		t.Fatalf("encoded = %q", encoded)
	}
}

func TestCodecEncodeTreatsRESP3AsRESP2(t *testing.T) {
	codec := Codec{}
	got := codec.Encode(protocol.MakeStatusReply("PONG"), iface.RESP3)
	want := []byte("+PONG\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
