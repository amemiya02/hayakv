package connection

import (
	"testing"

	"github.com/amemiya02/hayakv/internal/iface/redis"
)

func TestProtocolDefaultsToRESP2(t *testing.T) {
	c := NewConn(nil)
	if got := c.Protocol(); got != redis.RESP2 {
		t.Fatalf("default protocol = %d, want %d (RESP2)", got, redis.RESP2)
	}
	c.SetProtocol(redis.RESP3)
	if got := c.Protocol(); got != redis.RESP3 {
		t.Fatalf("after SetProtocol(RESP3) = %d, want %d", got, redis.RESP3)
	}
}
