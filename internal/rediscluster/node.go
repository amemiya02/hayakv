package rediscluster

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// node flag bits, mirroring Redis cluster node flags.
const (
	flagMyself uint32 = 1 << iota
	flagMaster
	flagSlave
	flagPFail // suspected failure (this node's local view)
	flagFail  // agreed failure
	flagHandshake
	flagNoAddr
)

// clusterNode is one node in the cluster (this node or a peer).
type clusterNode struct {
	id          string // 40 hex chars
	ip          string
	port        int    // client (data-plane) port
	cport       int    // cluster bus port = port + 10000
	flags       uint32
	masterID    string // if a replica, the id of its master
	configEpoch uint64
	pingSent    int64 // unix ms
	pongRecv    int64 // unix ms
	linkUp      bool
	slots       [slotCount / 8]byte // 2048-byte slot bitmap
}

func newNode(id, ip string, port int) *clusterNode {
	cport := port + 10000
	if cport > 65535 {
		cport = port - 10000 // fall back to port - 10000 if port + 10000 overflows
		if cport < 1 {
			cport = port // last resort: use the same port
		}
	}
	return &clusterNode{id: id, ip: ip, port: port, cport: cport, flags: flagMaster, linkUp: true}
}

func genNodeID() string {
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (n *clusterNode) addSlot(slot uint16)  { n.slots[slot/8] |= 1 << (slot % 8) }
func (n *clusterNode) delSlot(slot uint16)  { n.slots[slot/8] &^= 1 << (slot % 8) }
func (n *clusterNode) hasSlot(slot uint16) bool {
	return n.slots[slot/8]&(1<<(slot%8)) != 0
}

func (n *clusterNode) slotCount() int {
	c := 0
	for s := uint16(0); s < slotCount; s++ {
		if n.hasSlot(s) {
			c++
		}
	}
	return c
}

// slotRanges returns coalesced [start,end] inclusive ranges of owned slots.
func (n *clusterNode) slotRanges() [][2]uint16 {
	var out [][2]uint16
	inRun := false
	var start uint16
	for s := uint16(0); s < slotCount; s++ {
		if n.hasSlot(s) {
			if !inRun {
				inRun, start = true, s
			}
		} else if inRun {
			out = append(out, [2]uint16{start, s - 1})
			inRun = false
		}
	}
	if inRun {
		out = append(out, [2]uint16{start, slotCount - 1})
	}
	return out
}

func (n *clusterNode) isMyself() bool { return n.flags&flagMyself != 0 }

// flagString renders the comma-joined flag list for nodes.conf / CLUSTER NODES.
func (n *clusterNode) flagString() string {
	var parts []string
	if n.flags&flagMyself != 0 {
		parts = append(parts, "myself")
	}
	if n.flags&flagMaster != 0 {
		parts = append(parts, "master")
	}
	if n.flags&flagSlave != 0 {
		parts = append(parts, "slave")
	}
	if n.flags&flagPFail != 0 {
		parts = append(parts, "fail?")
	}
	if n.flags&flagFail != 0 {
		parts = append(parts, "fail")
	}
	if n.flags&flagHandshake != 0 {
		parts = append(parts, "handshake")
	}
	if n.flags&flagNoAddr != 0 {
		parts = append(parts, "noaddr")
	}
	if len(parts) == 0 {
		return "noflags"
	}
	return strings.Join(parts, ",")
}

// nodesLine renders one CLUSTER NODES / nodes.conf line for this node.
// Format: <id> <ip:port@cport> <flags> <master> <ping-sent> <pong-recv> <epoch> <link> [slot ranges...]
func (n *clusterNode) nodesLine() string {
	master := n.masterID
	if master == "" {
		master = "-"
	}
	link := "connected"
	if !n.linkUp {
		link = "disconnected"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s:%d@%d %s %s %d %d %d %s",
		n.id, n.ip, n.port, n.cport, n.flagString(), master,
		n.pingSent, n.pongRecv, n.configEpoch, link)
	for _, r := range n.slotRanges() {
		if r[0] == r[1] {
			fmt.Fprintf(&b, " %d", r[0])
		} else {
			fmt.Fprintf(&b, " %d-%d", r[0], r[1])
		}
	}
	return b.String()
}
