package rediscluster

import (
	"fmt"
	"strings"

	database "github.com/amemiya02/hayakv/internal/command"
	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// ClusterEngine decorates an inner StorageEngine with Redis Cluster redirection.
// It intercepts the CLUSTER/ASKING/READONLY/READWRITE commands and, for ordinary
// key commands, returns MOVED/ASK/CROSSSLOT when this node is not the slot owner.
type ClusterEngine struct {
	inner    iface.StorageEngine
	state    *clusterState
	commands *clusterCommands
}

// NewClusterEngine wraps inner with redirection driven by state. keysInSlot scans
// the inner keyspace; here we close over inner via a SCAN-like helper that the
// integration build supplies. For unit tests the engine is a stub, so we default
// to a no-op key lister and let Task 6/8 wire the real one through SetKeysInSlot.
func NewClusterEngine(inner iface.StorageEngine, state *clusterState) *ClusterEngine {
	ce := &ClusterEngine{inner: inner, state: state}
	ce.commands = newClusterCommands(state, func(slot uint16, count int) []string { return nil })
	return ce
}

// SetKeysInSlot installs the real key-enumeration callback (used by COUNT/GETKEYSINSLOT
// and migration). Wired in Task 6/8 against the engine's keyspace iterator.
func (ce *ClusterEngine) SetKeysInSlot(fn keysInSlotFunc) { ce.commands.keysInSlot = fn }

// Commands exposes the CLUSTER handler so the gossip bus (Task 7) can mutate state
// through the same object.
func (ce *ClusterEngine) Commands() *clusterCommands { return ce.commands }

func (ce *ClusterEngine) Exec(c iredis.Connection, cmdLine iface.CmdLine) iredis.Reply {
	if len(cmdLine) == 0 {
		return ce.inner.Exec(c, cmdLine)
	}
	name := strings.ToUpper(string(cmdLine[0]))
	switch name {
	case "CLUSTER":
		return ce.commands.handle(cmdLine)
	case "ASKING":
		setAsking(c, true)
		return protocol.MakeOkReply()
	case "READONLY":
		return protocol.MakeOkReply()
	case "READWRITE":
		return protocol.MakeOkReply()
	}

	keys := database.ExtractKeys(cmdLine)
	if len(keys) == 0 {
		// keyless (PING/INFO/HELLO/SELECT/...) — always local.
		return ce.inner.Exec(c, cmdLine)
	}

	// Compute the single slot all keys must share.
	slot := Key2Slot(string(keys[0]))
	for _, k := range keys[1:] {
		if Key2Slot(string(k)) != slot {
			return protocol.MakeErrReply("CROSSSLOT Keys in request don't hash to the same slot")
		}
	}

	if ce.state.imOwner(slot) {
		return ce.inner.Exec(c, cmdLine)
	}

	// Not the owner: ASK if we are importing this slot and the client sent ASKING
	// (Task 6 completes the ASK path); otherwise MOVED to the real owner.
	if r := ce.maybeAsk(c, slot, cmdLine); r != nil {
		return r
	}
	owner := ce.state.ownerOf(slot)
	if owner == nil {
		return protocol.MakeErrReply(fmt.Sprintf("CLUSTERDOWN Hash slot %d not served", slot))
	}
	return protocol.MakeErrReply(fmt.Sprintf("MOVED %d %s:%d", slot, owner.ip, owner.port))
}

func (ce *ClusterEngine) AfterClientClose(c iredis.Connection) { ce.inner.AfterClientClose(c) }
func (ce *ClusterEngine) Close()                               { ce.inner.Close() }

// maybeAsk is completed in Task 6. Returning nil means "fall through to MOVED".
func (ce *ClusterEngine) maybeAsk(c iredis.Connection, slot uint16, cmdLine iface.CmdLine) iredis.Reply {
	return nil
}

var _ iface.StorageEngine = (*ClusterEngine)(nil)
