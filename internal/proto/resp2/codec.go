package resp2

import (
	"io"

	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/parser"
)

// Codec implements iface.ProtocolCodec for the RESP2 wire format.
type Codec struct{}

// DecodeStream reads RESP2 frames from reader and sends them on the returned channel.
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

// DecodeOne parses a single RESP2 frame from data.
func (Codec) DecodeOne(data []byte) (iredis.Reply, error) {
	return parser.ParseOne(data)
}

// Encode serialises a reply. In M0 RESP3 is treated identically to RESP2.
func (Codec) Encode(reply iredis.Reply, _ iface.RespVersion) []byte {
	if reply == nil {
		return nil
	}
	return reply.ToBytes()
}
