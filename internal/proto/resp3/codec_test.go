package resp3

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func TestCodecEncodeBranchesOnVersion(t *testing.T) {
	var c iface.ProtocolCodec = Codec{}
	nb := protocol.MakeNullBulkReply()
	if got := string(c.Encode(nb, iface.RESP2)); got != "$-1\r\n" {
		t.Errorf("RESP2 null = %q, want $-1", got)
	}
	if got := string(c.Encode(nb, iface.RESP3)); got != "_\r\n" {
		t.Errorf("RESP3 null = %q, want _", got)
	}
}
