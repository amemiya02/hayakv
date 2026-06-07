package rediscluster

import (
	"sync"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
)

var (
	askingMu sync.Mutex
	asking   = map[string]bool{} // remoteAddr -> one-shot ASKING set
)

func setAsking(c iredis.Connection, v bool) {
	if c == nil {
		return
	}
	askingMu.Lock()
	defer askingMu.Unlock()
	if v {
		asking[c.RemoteAddr()] = true
	} else {
		delete(asking, c.RemoteAddr())
	}
}

// takeAsking reads and clears the one-shot ASKING flag for c.
func takeAsking(c iredis.Connection) bool {
	if c == nil {
		return false
	}
	askingMu.Lock()
	defer askingMu.Unlock()
	v := asking[c.RemoteAddr()]
	delete(asking, c.RemoteAddr())
	return v
}
