package resp3

import (
	"io"

	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/parser"
)

// Codec implements iface.ProtocolCodec. Request framing matches RESP2, so
// decoding reuses the RESP2 parser; only Encode is version-aware.
type Codec struct{}

func (Codec) DecodeStream(reader io.Reader) <-chan iface.ProtocolPayload {
	out := make(chan iface.ProtocolPayload)
	in := parser.ParseStream(reader)
	go func() {
		defer close(out)
		for payload := range in {
			if payload == nil {
				out <- iface.ProtocolPayload{Err: io.ErrUnexpectedEOF}
				continue
			}
			out <- iface.ProtocolPayload{Reply: payload.Data, Err: payload.Err}
		}
	}()
	return out
}

func (Codec) DecodeOne(data []byte) (iredis.Reply, error) { return parser.ParseOne(data) }

func (Codec) Encode(reply iredis.Reply, resp iface.RespVersion) []byte {
	if reply == nil {
		return nil
	}
	if resp == iface.RESP3 {
		return EncodeRESP3(reply)
	}
	return reply.ToBytes()
}
