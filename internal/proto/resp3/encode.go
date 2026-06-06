package resp3

import (
	"bytes"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
)

// EncodeRESP3 emits RESP3 bytes for any reply. RESP3-native replies encode
// themselves; legacy RESP2 replies are translated.
func EncodeRESP3(reply iredis.Reply) []byte {
	switch reply.(type) {
	case *NullReply, *BoolReply, *DoubleReply, *BigNumberReply,
		*MapReply, *SetReply, *PushReply, *VerbatimReply:
		return reply.ToBytes()
	default:
		return EncodeRESP3FromRESP2(reply.ToBytes())
	}
}

// EncodeRESP3FromRESP2 rewrites the two RESP2 null forms to RESP3 null `_`.
// All other RESP2 frames are valid RESP3 and pass through unchanged.
func EncodeRESP3FromRESP2(b []byte) []byte {
	if bytes.Equal(b, []byte("$-1\r\n")) || bytes.Equal(b, []byte("*-1\r\n")) {
		return []byte("_\r\n")
	}
	return b
}
