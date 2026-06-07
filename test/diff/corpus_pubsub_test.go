package diff

import (
	"bytes"
	"testing"
)

func TestDifferentialPubSub(t *testing.T) {
	hayakvAddr, stop := startHayakv(t)
	defer stop()
	redisAddr, stopR := startRedis8(t)
	defer stopR()
	sc := MultiConnScenario{Name: "subscribe publish", Conns: 2, Steps: []ConnStep{
		{Conn: 1, Args: []string{"SUBSCRIBE", "ch"}},
		{Conn: 0, Args: []string{"PUBLISH", "ch", "hello"}},
		{Conn: 1, Args: []string{"UNSUBSCRIBE", "ch"}},
	}}
	h := runScenarioMultiConn(t, hayakvAddr, sc)
	r := runScenarioMultiConn(t, redisAddr, sc)
	if len(h) != len(r) {
		t.Fatalf("len h=%d r=%d", len(h), len(r))
	}
	for i := range h {
		if !bytes.Equal(h[i], r[i]) {
			t.Fatalf("step %d\nhayakv: %q\nredis: %q", i, h[i], r[i])
		}
	}
}
