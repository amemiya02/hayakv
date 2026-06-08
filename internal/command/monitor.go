package database

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/pubsub"
)

// Monitor registry — connections that should receive MONITOR feeds.
var (
	monitorConns   = make(map[uint64]redis.Connection)
	monitorConnsMu sync.RWMutex
)

func registerMonitor(c redis.Connection) {
	monitorConnsMu.Lock()
	defer monitorConnsMu.Unlock()
	monitorConns[c.ClientID()] = c
}

func unregisterMonitor(id uint64) {
	monitorConnsMu.Lock()
	defer monitorConnsMu.Unlock()
	delete(monitorConns, id)
}

func feedMonitors(dbIndex int, cmdLine [][]byte, addr string) {
	monitorConnsMu.RLock()
	defer monitorConnsMu.RUnlock()

	if len(monitorConns) == 0 {
		return
	}

	now := time.Now()
	ts := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)

	// Format: "timestamp [dbindex addr] "CMD" "arg" ..."
	var parts []string
	for _, arg := range cmdLine {
		parts = append(parts, fmt.Sprintf("%q", arg))
	}
	line := fmt.Sprintf("%s [%d %s] %s\r\n", ts, dbIndex, addr, strings.Join(parts, " "))

	for _, mc := range monitorConns {
		_, _ = mc.Write([]byte("+" + line))
	}
}

// execMonitor handles the MONITOR command.
func execMonitor(server *Server, c redis.Connection) redis.Reply {
	registerMonitor(c)
	return protocol.MakeStatusReply("OK")
}

// execReset handles the RESET command.
func execReset(server *Server, c redis.Connection) redis.Reply {
	// Clear subscriptions
	pubsub.UnsubscribeAll(server.hub, c)
	// Clear monitor status
	unregisterMonitor(c.ClientID())
	// Clear MULTI state
	c.SetMultiState(false)
	// Clear client name
	c.SetClientName("")
	// Select DB 0
	c.SelectDB(0)
	// Re-require auth: clear password so next command needs re-auth
	c.SetPassword("")

	return protocol.MakeStatusReply("RESET")
}
