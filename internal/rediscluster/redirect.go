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
	inner     iface.StorageEngine
	state     *clusterState
	commands  *clusterCommands
	keyExists keyExistsFunc
}

// NewClusterEngine wraps inner with redirection driven by state. keysInSlot scans
// the inner keyspace; here we close over inner via a SCAN-like helper that the
// integration build supplies. For unit tests the engine is a stub, so we default
// to a no-op key lister and let Task 6/8 wire the real one through SetKeysInSlot.
func NewClusterEngine(inner iface.StorageEngine, state *clusterState) *ClusterEngine {
	ce := &ClusterEngine{inner: inner, state: state}
	ce.commands = newClusterCommands(state, func(slot uint16, count int) []string { return nil })
	ce.keyExists = func(string) bool { return false }
	return ce
}

// SetKeysInSlot installs the real key-enumeration callback (used by COUNT/GETKEYSINSLOT
// and migration). Wired in Task 6/8 against the engine's keyspace iterator.
func (ce *ClusterEngine) SetKeysInSlot(fn keysInSlotFunc) { ce.commands.keysInSlot = fn }

// SetKeyExists installs the key-existence probe used by ASK redirection.
// Wired in Task 8 against the engine's keyspace; defaults to always-false.
func (ce *ClusterEngine) SetKeyExists(fn keyExistsFunc) { ce.keyExists = fn }

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
	case "MIGRATE":
		return migrateReply(cmdLine[1:])
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
		if r := ce.maybeAsk(c, slot, cmdLine); r != nil {
			return r
		}
		return ce.inner.Exec(c, cmdLine)
	}

	// Not the owner: ASK if we are importing this slot and the client sent ASKING.
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

// NewClusterEngineFromConfig builds a ClusterEngine, generating or reloading the
// node identity/slot map from confPath.
func NewClusterEngineFromConfig(inner iface.StorageEngine, ip string, port int, confPath string) (*ClusterEngine, error) {
	state := newClusterState(ip, port, confPath)
	if err := state.load(); err != nil {
		return nil, err
	}
	if err := state.save(); err != nil { // ensure the file exists with our identity
		return nil, err
	}
	return NewClusterEngine(inner, state), nil
}

// maybeAsk handles ASK redirection during slot migration.
// Returning nil means "fall through to MOVED" (or serve locally if owner).
func (ce *ClusterEngine) maybeAsk(c iredis.Connection, slot uint16, cmdLine iface.CmdLine) iredis.Reply {
	// Case A: we own the slot and it is MIGRATING out; if the (first) key is gone
	// locally, redirect the client to the import target with ASK.
	if target := ce.state.migratingTo(slot); target != "" && ce.state.imOwner(slot) {
		keys := database.ExtractKeys(cmdLine)
		allPresent := true
		for _, k := range keys {
			if !ce.keyExists(string(k)) {
				allPresent = false
				break
			}
		}
		if !allPresent {
			if tn := ce.state.nodeByID(target); tn != nil {
				return protocol.MakeErrReply(fmt.Sprintf("ASK %d %s:%d", slot, tn.ip, tn.port))
			}
		}
		return nil // keys present locally: serve here (caller proceeds to inner)
	}
	// Case B: we do NOT own the slot but we are IMPORTING it and the client sent
	// ASKING (one-shot): serve locally instead of MOVED.
	if src := ce.state.importingFrom(slot); src != "" {
		if takeAsking(c) {
			return execLocal(ce.inner, c, cmdLine)
		}
	}
	return nil // fall through to MOVED
}

// execLocal is a thin wrapper to make the ASK-serve path explicit/testable.
func execLocal(inner iface.StorageEngine, c iredis.Connection, cmdLine iface.CmdLine) iredis.Reply {
	return inner.Exec(c, cmdLine)
}

var _ iface.StorageEngine = (*ClusterEngine)(nil)
