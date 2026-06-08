package database

import (
	"strconv"
	"strings"
	"sync"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/proto/resp3"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

// invalidationTable maps key -> set of client IDs tracking that key.
type invalidationTable struct {
	mu    sync.RWMutex
	table map[string]map[uint64]struct{}
}

var invTable = &invalidationTable{
	table: make(map[string]map[uint64]struct{}),
}

func (t *invalidationTable) track(key string, clientID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.table[key] == nil {
		t.table[key] = make(map[uint64]struct{})
	}
	t.table[key][clientID] = struct{}{}
}

func (t *invalidationTable) untrack(key string, clientID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if clients, ok := t.table[key]; ok {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(t.table, key)
		}
	}
}

func (t *invalidationTable) untrackAll(clientID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, clients := range t.table {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(t.table, key)
		}
	}
}

// notifyTrackedKeys sends invalidation messages for the given keys.
func (t *invalidationTable) notifyTrackedKeys(server *Server, keys []string, sourceClientID uint64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, key := range keys {
		clients, ok := t.table[key]
		if !ok {
			continue
		}
		for clientID := range clients {
			if clientID == sourceClientID {
				continue
			}
			conn := connection.ClientByID(clientID)
			if conn == nil {
				continue
			}
			if conn.Protocol() == redis.RESP3 {
				push := resp3.MakePushReply([]redis.Reply{
					protocol.MakeBulkReply([]byte("invalidate")),
					protocol.MakeMultiBulkReply([][]byte{[]byte(key)}),
				})
				_, _ = conn.Write(push.ToBytes())
			} else {
				redirectID := conn.RedirectID()
				if redirectID == 0 {
					redirectID = clientID
				}
				target := connection.ClientByID(redirectID)
				if target != nil {
					msg := protocol.MakeMultiBulkReply([][]byte{
						[]byte("message"),
						[]byte("__redis__:invalidate"),
						[]byte(key),
					})
					_, _ = target.Write(msg.ToBytes())
				}
			}
		}
	}
}

// execClientTracking handles CLIENT TRACKING ON|OFF.
func execClientTracking(server *Server, c redis.Connection, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'client|tracking' command")
	}

	mode := strings.ToUpper(string(args[0]))
	if mode == "ON" {
		c.SetTracking(true)
		for i := 1; i < len(args); i++ {
			opt := strings.ToUpper(string(args[i]))
			switch opt {
			case "REDIRECT":
				if i+1 < len(args) {
					id, err := strconv.ParseUint(string(args[i+1]), 10, 64)
					if err == nil {
						c.SetRedirectID(id)
					}
					i++
				}
			case "NOLOOP":
				c.SetNoLoop(true)
			case "OPTIN":
				c.SetTrackingMode(1)
			case "OPTOUT":
				c.SetTrackingMode(2)
			}
		}
		return protocol.MakeOkReply()
	} else if mode == "OFF" {
		c.SetTracking(false)
		c.SetTrackingMode(0)
		c.SetNoLoop(false)
		c.SetRedirectID(0)
		invTable.untrackAll(c.ClientID())
		return protocol.MakeOkReply()
	}
	return protocol.MakeErrReply("ERR invalid TRACKING mode")
}

// trackReadKeys registers keys as tracked for a connection (called on reads).
func trackReadKeys(c redis.Connection, keys ...string) {
	if !c.IsTracking() {
		return
	}
	for _, key := range keys {
		invTable.track(key, c.ClientID())
	}
}

// notifyWriteKeys notifies tracked clients of key mutations (called on writes).
func notifyWriteKeys(server *Server, keys []string, sourceClientID uint64) {
	invTable.notifyTrackedKeys(server, keys, sourceClientID)
}

// clientTrackingInfo returns tracking info for the connection.
func clientTrackingInfo(c redis.Connection) redis.Reply {
	flags := "off"
	if c.IsTracking() {
		flags = "on"
	}
	return protocol.MakeMultiBulkReply([][]byte{
		[]byte("flags"),
		[]byte(flags),
	})
}

// clientGetRedirect returns the redirect client ID.
func clientGetRedirect(c redis.Connection) redis.Reply {
	return protocol.MakeIntReply(int64(c.RedirectID()))
}

// clientCaching handles CLIENT CACHING YES|NO (for OPTIN/OPTOUT mode).
func clientCaching(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'client|caching' command")
	}
	// In OPTIN mode, CLIENT CACHING YES enables tracking for the next command.
	// This is a simplified implementation — just acknowledge.
	return protocol.MakeOkReply()
}
