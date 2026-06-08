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

// trackKey builds a table key scoped by DB index.
func trackKey(dbIndex int, key string) string {
	return strconv.Itoa(dbIndex) + ":" + key
}

// invalidationTable maps "dbIndex:key" -> set of client IDs tracking that key.
type invalidationTable struct {
	mu    sync.RWMutex
	table map[string]map[uint64]struct{}
	// bcast maps "dbIndex:prefix" -> set of client IDs in BCAST mode with that prefix.
	bcast map[string]map[uint64]struct{}
}

var invTable = &invalidationTable{
	table: make(map[string]map[uint64]struct{}),
	bcast: make(map[string]map[uint64]struct{}),
}

func (t *invalidationTable) track(dbIndex int, key string, clientID uint64) {
	tk := trackKey(dbIndex, key)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.table[tk] == nil {
		t.table[tk] = make(map[uint64]struct{})
	}
	t.table[tk][clientID] = struct{}{}
}

func (t *invalidationTable) untrack(dbIndex int, key string, clientID uint64) {
	tk := trackKey(dbIndex, key)
	t.mu.Lock()
	defer t.mu.Unlock()
	if clients, ok := t.table[tk]; ok {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(t.table, tk)
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
	for key, clients := range t.bcast {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(t.bcast, key)
		}
	}
}

// trackBcast registers a BCAST prefix for a client.
func (t *invalidationTable) trackBcast(dbIndex int, prefix string, clientID uint64) {
	tk := trackKey(dbIndex, prefix)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.bcast[tk] == nil {
		t.bcast[tk] = make(map[uint64]struct{})
	}
	t.bcast[tk][clientID] = struct{}{}
}

// untrackBcast removes a BCAST prefix for a client.
func (t *invalidationTable) untrackBcast(dbIndex int, prefix string, clientID uint64) {
	tk := trackKey(dbIndex, prefix)
	t.mu.Lock()
	defer t.mu.Unlock()
	if clients, ok := t.bcast[tk]; ok {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(t.bcast, tk)
		}
	}
}

// untrackDB removes all tracking entries for a specific DB index and sends
// null-array invalidation to affected clients (FLUSHDB/FLUSHALL semantics).
func (t *invalidationTable) untrackDB(server *Server, dbIndex int) {
	t.mu.Lock()
	prefix := strconv.Itoa(dbIndex) + ":"
	affected := make(map[uint64]struct{})
	for key, clients := range t.table {
		if strings.HasPrefix(key, prefix) {
			for cid := range clients {
				affected[cid] = struct{}{}
			}
			delete(t.table, key)
		}
	}
	for key, clients := range t.bcast {
		if strings.HasPrefix(key, prefix) {
			for cid := range clients {
				affected[cid] = struct{}{}
			}
			delete(t.bcast, key)
		}
	}
	t.mu.Unlock()

	// Redis sends a null-array invalidation for FLUSHDB/FLUSHALL.
	for clientID := range affected {
		conn := connection.ClientByID(clientID)
		if conn == nil {
			continue
		}
		if conn.Protocol() == redis.RESP3 {
			push := resp3.MakePushReply([]redis.Reply{
				protocol.MakeBulkReply([]byte("invalidate")),
				protocol.MakeMultiBulkReply(nil), // null array = flush
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
					nil, // null = flush
				})
				_, _ = target.Write(msg.ToBytes())
			}
		}
	}
}

// notifyTrackedKeys sends invalidation messages for the given keys.
// This includes both exact-match tracking (read-path) and BCAST prefix tracking.
func (t *invalidationTable) notifyTrackedKeys(server *Server, dbIndex int, keys []string, sourceClientID uint64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Collect all affected client IDs (from exact-match + BCAST) per key.
	type clientSet = map[uint64]struct{}
	perKey := make(map[string]clientSet, len(keys))

	for _, key := range keys {
		affected := make(clientSet)
		// Exact-match tracking
		tk := trackKey(dbIndex, key)
		if clients, ok := t.table[tk]; ok {
			for cid := range clients {
				affected[cid] = struct{}{}
			}
		}
		// BCAST prefix tracking: scan all registered prefixes for this DB.
		dbPrefix := strconv.Itoa(dbIndex) + ":"
		for bkey, clients := range t.bcast {
			if !strings.HasPrefix(bkey, dbPrefix) {
				continue
			}
			pfx := bkey[len(dbPrefix):]
			if pfx == "" || strings.HasPrefix(key, pfx) {
				for cid := range clients {
					affected[cid] = struct{}{}
				}
			}
		}
		if len(affected) > 0 {
			perKey[key] = affected
		}
	}

	for key, affected := range perKey {
		for clientID := range affected {
			conn := connection.ClientByID(clientID)
			if conn == nil {
				continue
			}
			// Skip self-notification only when NOLOOP is set
			if clientID == sourceClientID && conn.NoLoop() {
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
		var bcastPrefixes []string
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
			case "BCAST":
				c.SetBcastMode(true)
			case "PREFIX":
				if i+1 < len(args) {
					pfx := string(args[i+1])
					bcastPrefixes = append(bcastPrefixes, pfx)
					i++
				}
			}
		}
		// RESP2 requires REDIRECT; without it, invalidations have nowhere to go.
		if c.Protocol() != redis.RESP3 && c.RedirectID() == 0 {
			c.SetTracking(false)
			return protocol.MakeErrReply("ERR Client tracking can be enabled only in RESP3 mode or when a redirection client is specified via the 'REDIRECT' option")
		}
		// Register BCAST prefixes for all DBs.
		if len(bcastPrefixes) > 0 {
			c.SetBcastPrefixes(bcastPrefixes)
			dbCount := len(server.dbSet)
			for _, pfx := range bcastPrefixes {
				for db := 0; db < dbCount; db++ {
					invTable.trackBcast(db, pfx, c.ClientID())
				}
			}
		}
		return protocol.MakeOkReply()
	} else if mode == "OFF" {
		// Unregister BCAST prefixes before clearing state.
		if c.BcastMode() {
			dbCount := len(server.dbSet)
			for _, pfx := range c.BcastPrefixes() {
				for db := 0; db < dbCount; db++ {
					invTable.untrackBcast(db, pfx, c.ClientID())
				}
			}
		}
		c.SetTracking(false)
		c.SetTrackingMode(0)
		c.SetNoLoop(false)
		c.SetRedirectID(0)
		c.SetBcastMode(false)
		c.SetBcastPrefixes(nil)
		c.SetCachingNext(false)
		invTable.untrackAll(c.ClientID())
		return protocol.MakeOkReply()
	}
	return protocol.MakeErrReply("ERR invalid TRACKING mode")
}

// trackReadKeys registers keys as tracked for a connection (called on reads).
// BCAST clients skip read-path tracking — they receive invalidations via prefix matching.
func trackReadKeys(c redis.Connection, dbIndex int, keys ...string) {
	if !c.IsTracking() {
		return
	}
	// BCAST clients don't do read-path tracking; they get prefix-based invalidations.
	if c.BcastMode() {
		return
	}
	// In OPTIN mode, only track if CLIENT CACHING YES was called before this command.
	if c.TrackingMode() == 1 && !c.CachingNext() {
		return
	}
	// In OPTOUT mode, skip tracking if CLIENT CACHING NO was called before this command.
	if c.TrackingMode() == 2 && c.CachingNext() {
		return
	}
	for _, key := range keys {
		invTable.track(dbIndex, key, c.ClientID())
	}
	// Reset caching-next flag after each command.
	c.SetCachingNext(false)
}

// notifyWriteKeys notifies tracked clients of key mutations (called on writes).
func notifyWriteKeys(server *Server, dbIndex int, keys []string, sourceClientID uint64) {
	invTable.notifyTrackedKeys(server, dbIndex, keys, sourceClientID)
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
	if !c.IsTracking() {
		return protocol.MakeErrReply("ERR CLIENT CACHING is not available when tracking is disabled")
	}
	opt := strings.ToUpper(string(args[0]))
	switch opt {
	case "YES":
		c.SetCachingNext(true)
	case "NO":
		c.SetCachingNext(false)
	default:
		return protocol.MakeErrReply("ERR invalid CLIENT CACHING option, must be YES or NO")
	}
	return protocol.MakeOkReply()
}
