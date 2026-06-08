package rediscluster

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"

	"github.com/amemiya02/hayakv/config"
	"sync"
	"time"
)

const (
	clusterMsgSig = "RCmb" // Redis Cluster message bus signature
	clusterMsgVer = 1

	msgTypePing uint16 = 0
	msgTypePong uint16 = 1
	msgTypeMeet uint16 = 2
	msgTypeFail uint16 = 3

	msgTypeFailoverAuthRequest uint16 = 4 // replica requests vote from master
	msgTypeFailoverAuthAck     uint16 = 5 // master grants vote to replica
	msgTypePublishShard        uint16 = 6 // sharded pub/sub propagation

	idLen = 40
	// headerLen layout (fixed):
	//   sig[4] totalLen[4] ver[2] type[2] count[2] currentEpoch[8] configEpoch[8]
	//   port[4] cport[4] flags[4] senderID[40] slots[2048]
	headerLen = 4 + 4 + 2 + 2 + 2 + 8 + 8 + 4 + 4 + 4 + idLen + (slotCount / 8)
)

// clusterMsgHeader is the fixed part of a bus message (little-endian on the wire).
type clusterMsgHeader struct {
	totalLen     uint32
	version      uint16
	msgType      uint16
	gossipCount  uint16
	currentEpoch uint64
	configEpoch  uint64
	port         uint32
	cport        uint32
	flags        uint32
	senderID     string
	slots        [slotCount / 8]byte
}

// gossipEntry is one peer summary in the gossip section.
//
//	id[40] ip[46] port[4] cport[4] flags[4]
const (
	ipFieldLen   = 46
	gossipEntLen = idLen + ipFieldLen + 4 + 4 + 4
)

type gossipEntry struct {
	id    string
	ip    string
	port  uint32
	cport uint32
	flags uint32
}

func putID(dst []byte, id string) {
	b := []byte(id)
	if len(b) > idLen {
		b = b[:idLen]
	}
	copy(dst, b)
	for i := len(b); i < idLen; i++ {
		dst[i] = 0
	}
}

func getID(src []byte) string {
	n := 0
	for n < len(src) && src[n] != 0 {
		n++
	}
	return string(src[:n])
}

func encodeHeader(h *clusterMsgHeader) []byte {
	buf := make([]byte, headerLen)
	copy(buf[0:4], clusterMsgSig)
	binary.LittleEndian.PutUint32(buf[4:8], h.totalLen)
	binary.LittleEndian.PutUint16(buf[8:10], clusterMsgVer)
	binary.LittleEndian.PutUint16(buf[10:12], h.msgType)
	binary.LittleEndian.PutUint16(buf[12:14], h.gossipCount)
	binary.LittleEndian.PutUint64(buf[14:22], h.currentEpoch)
	binary.LittleEndian.PutUint64(buf[22:30], h.configEpoch)
	binary.LittleEndian.PutUint32(buf[30:34], h.port)
	binary.LittleEndian.PutUint32(buf[34:38], h.cport)
	binary.LittleEndian.PutUint32(buf[38:42], h.flags)
	putID(buf[42:42+idLen], h.senderID)
	copy(buf[42+idLen:], h.slots[:])
	return buf
}

func decodeHeader(buf []byte) (*clusterMsgHeader, error) {
	if len(buf) < headerLen {
		return nil, errors.New("short cluster msg header")
	}
	if string(buf[0:4]) != clusterMsgSig {
		return nil, fmt.Errorf("bad signature %q", buf[0:4])
	}
	h := &clusterMsgHeader{
		totalLen:     binary.LittleEndian.Uint32(buf[4:8]),
		version:      binary.LittleEndian.Uint16(buf[8:10]),
		msgType:      binary.LittleEndian.Uint16(buf[10:12]),
		gossipCount:  binary.LittleEndian.Uint16(buf[12:14]),
		currentEpoch: binary.LittleEndian.Uint64(buf[14:22]),
		configEpoch:  binary.LittleEndian.Uint64(buf[22:30]),
		port:         binary.LittleEndian.Uint32(buf[30:34]),
		cport:        binary.LittleEndian.Uint32(buf[34:38]),
		flags:        binary.LittleEndian.Uint32(buf[38:42]),
		senderID:     getID(buf[42 : 42+idLen]),
	}
	copy(h.slots[:], buf[42+idLen:headerLen])
	return h, nil
}

func encodeGossip(entries []gossipEntry) []byte {
	buf := make([]byte, len(entries)*gossipEntLen)
	for i, e := range entries {
		off := i * gossipEntLen
		putID(buf[off:off+idLen], e.id)
		ip := []byte(e.ip)
		if len(ip) > ipFieldLen {
			ip = ip[:ipFieldLen]
		}
		copy(buf[off+idLen:off+idLen+ipFieldLen], ip)
		binary.LittleEndian.PutUint32(buf[off+idLen+ipFieldLen:], e.port)
		binary.LittleEndian.PutUint32(buf[off+idLen+ipFieldLen+4:], e.cport)
		binary.LittleEndian.PutUint32(buf[off+idLen+ipFieldLen+8:], e.flags)
	}
	return buf
}

func decodeGossip(buf []byte, count int) []gossipEntry {
	out := make([]gossipEntry, 0, count)
	for i := 0; i < count && (i+1)*gossipEntLen <= len(buf); i++ {
		off := i * gossipEntLen
		ipRaw := buf[off+idLen : off+idLen+ipFieldLen]
		n := 0
		for n < len(ipRaw) && ipRaw[n] != 0 {
			n++
		}
		out = append(out, gossipEntry{
			id:    getID(buf[off : off+idLen]),
			ip:    string(ipRaw[:n]),
			port:  binary.LittleEndian.Uint32(buf[off+idLen+ipFieldLen:]),
			cport: binary.LittleEndian.Uint32(buf[off+idLen+ipFieldLen+4:]),
			flags: binary.LittleEndian.Uint32(buf[off+idLen+ipFieldLen+8:]),
		})
	}
	return out
}

// gossipBus is the binary cluster-bus listener/dialer for one node.
type gossipBus struct {
	state  *clusterState
	ln     net.Listener
	mu     sync.Mutex
	closed bool
	stopCh chan struct{}
}

func newGossipBus(state *clusterState) *gossipBus {
	return &gossipBus{state: state, stopCh: make(chan struct{})}
}

func (g *gossipBus) start() error {
	addr := fmt.Sprintf("%s:%d", g.state.self.ip, g.state.self.cport)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	g.ln = ln
	go g.acceptLoop()
	go g.pingLoop()
	return nil
}

func (g *gossipBus) stop() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	close(g.stopCh)
	g.mu.Unlock()
	if g.ln != nil {
		_ = g.ln.Close()
	}
}

func (g *gossipBus) acceptLoop() {
	for {
		conn, err := g.ln.Accept()
		if err != nil {
			select {
			case <-g.stopCh:
				return
			default:
				return
			}
		}
		go g.handleConn(conn)
	}
}

func (g *gossipBus) handleConn(conn net.Conn) {
	defer conn.Close()
	hdrBuf := make([]byte, headerLen)
	if _, err := io.ReadFull(conn, hdrBuf); err != nil {
		return
	}
	h, err := decodeHeader(hdrBuf)
	if err != nil {
		return
	}
	gossipBuf := make([]byte, int(h.gossipCount)*gossipEntLen)
	if len(gossipBuf) > 0 {
		if _, err := io.ReadFull(conn, gossipBuf); err != nil {
			return
		}
	}
	// Extract sender IP from the connection's remote address.
	senderIP := ""
	if addr := conn.RemoteAddr(); addr != nil {
		if tcpAddr, ok := addr.(*net.TCPAddr); ok {
			senderIP = tcpAddr.IP.String()
		}
	}
	g.mergeFromMessage(h, decodeGossip(gossipBuf, int(h.gossipCount)), senderIP)

	// Reply to PING/MEET with a PONG carrying our own view.
	if h.msgType == msgTypePing || h.msgType == msgTypeMeet {
		pong := g.buildMessage(msgTypePong)
		_, _ = conn.Write(pong)
	}

	// Handle FAIL message: mark the named node as definitively failed.
	if h.msgType == msgTypeFail {
		g.handleFailMessage(h)
	}

	// Handle failover auth request: a master decides whether to grant a vote.
	if h.msgType == msgTypeFailoverAuthRequest {
		g.handleAuthRequest(h)
	}

	// Handle failover auth ack: the requesting replica tallies a vote.
	if h.msgType == msgTypeFailoverAuthAck {
		g.handleAuthAck(h, decodeGossip(gossipBuf, int(h.gossipCount)))
	}
}

// mergeFromMessage updates local state with the sender's identity + slots and any
// gossiped peers it didn't already know.  senderIP is the remote address from
// the TCP connection (may be "" if unavailable).
func (g *gossipBus) mergeFromMessage(h *clusterMsgHeader, entries []gossipEntry, senderIP string) {
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if h.currentEpoch > g.state.epoch {
		g.state.epoch = h.currentEpoch
	}
	sender := g.state.nodes[h.senderID]
	if sender == nil && h.senderID != "" && h.senderID != g.state.self.id {
		sender = &clusterNode{id: h.senderID, flags: flagMaster, linkUp: true}
		g.state.nodes[h.senderID] = sender
	}
	if sender != nil {
		sender.port = int(h.port)
		sender.cport = int(h.cport)
		sender.configEpoch = h.configEpoch
		sender.pongRecv = time.Now().UnixMilli()
		sender.linkUp = true
		// Record the sender's IP from the TCP connection if not already set.
		if sender.ip == "" && senderIP != "" {
			sender.ip = senderIP
		}
		// Adopt the sender's slot ownership only if its configEpoch is higher
		// than the current owner's (Redis cluster epoch-based resolution).
		for s := uint16(0); s < slotCount; s++ {
			owns := h.slots[s/8]&(1<<(s%8)) != 0
			if owns {
				old := g.state.slots[s]
				if old == sender {
					continue // already own it
				}
				if old != nil && old.configEpoch >= h.configEpoch {
					continue // current owner has equal or higher epoch; skip
				}
				if old != nil {
					old.delSlot(s)
				}
				g.state.slots[s] = sender
				sender.addSlot(s)
			}
		}
	}
	for _, e := range entries {
		if e.id == "" || e.id == g.state.self.id {
			continue
		}
		node, existed := g.state.nodes[e.id]
		if !existed {
			g.state.nodes[e.id] = &clusterNode{
				id: e.id, ip: e.ip, port: int(e.port), cport: int(e.cport),
				flags: e.flags, linkUp: true,
			}
			continue
		}
		// Update existing node with failure status from the sender's gossip.
		if e.flags&uint32(flagFail) != 0 {
			// FAIL is authoritative (majority agreed) — adopt it.
			node.flags &^= flagPFail
			node.flags |= flagFail
		} else if e.flags&uint32(flagPFail) != 0 && node.flags&flagFail == 0 {
			// PFAIL from this sender — record a failure report.
			node.flags |= flagPFail
			if g.state.failureReports != nil {
				g.state.failureReports.addReport(e.id, h.senderID)
			}
		}
	}
}

// buildMessage assembles a header + gossip section reflecting our current view.
// FAIL'd nodes are always included first (up to maxGossip) so that failure
// propagation is not lost when the cluster has more nodes than the sample cap.
const maxGossip = 3

func (g *gossipBus) buildMessage(msgType uint16) []byte {
	g.state.mu.RLock()
	h := clusterMsgHeader{
		msgType:      msgType,
		currentEpoch: g.state.epoch,
		configEpoch:  g.state.self.configEpoch,
		port:         uint32(g.state.self.port),
		cport:        uint32(g.state.self.cport),
		flags:        g.state.self.flags,
		senderID:     g.state.self.id,
		slots:        g.state.self.slots,
	}
	// Prioritize FAIL'd nodes so failure propagation is reliable.
	var entries []gossipEntry
	for _, n := range g.state.nodes {
		if n.id == g.state.self.id {
			continue
		}
		if n.flags&flagFail != 0 {
			entries = append(entries, gossipEntry{
				id: n.id, ip: n.ip, port: uint32(n.port), cport: uint32(n.cport), flags: n.flags,
			})
			if len(entries) >= maxGossip {
				break
			}
		}
	}
	// Fill remaining slots with non-FAIL nodes.
	if len(entries) < maxGossip {
		for _, n := range g.state.nodes {
			if n.id == g.state.self.id || n.flags&flagFail != 0 {
				continue
			}
			entries = append(entries, gossipEntry{
				id: n.id, ip: n.ip, port: uint32(n.port), cport: uint32(n.cport), flags: n.flags,
			})
			if len(entries) >= maxGossip {
				break
			}
		}
	}
	g.state.mu.RUnlock()
	h.gossipCount = uint16(len(entries))
	body := encodeGossip(entries)
	h.totalLen = uint32(headerLen + len(body))
	return append(encodeHeader(&h), body...)
}

// meet dials a peer's bus port and sends a MEET, then reads its PONG and merges.
func (g *gossipBus) meet(ip string, port, cport int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, cport), 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write(g.buildMessage(msgTypeMeet)); err != nil {
		return err
	}
	hdrBuf := make([]byte, headerLen)
	if _, err := io.ReadFull(conn, hdrBuf); err != nil {
		return err
	}
	h, err := decodeHeader(hdrBuf)
	if err != nil {
		return err
	}
	gossipBuf := make([]byte, int(h.gossipCount)*gossipEntLen)
	if len(gossipBuf) > 0 {
		if _, err := io.ReadFull(conn, gossipBuf); err != nil {
			return err
		}
	}
	// Record the dialed peer's address against the id we just learned.
	g.state.mu.Lock()
	if n := g.state.nodes[h.senderID]; n == nil && h.senderID != "" {
		g.state.nodes[h.senderID] = &clusterNode{id: h.senderID, ip: ip, port: port, cport: cport, flags: flagMaster, linkUp: true}
	}
	g.state.mu.Unlock()
	g.mergeFromMessage(h, decodeGossip(gossipBuf, int(h.gossipCount)), ip)
	return nil
}

// pingLoop periodically PINGs a random known peer to keep links fresh.
func (g *gossipBus) pingLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.pingOnePeer()
			// Check for timed-out peers (PFAIL detection)
			if timeout := config.Properties.ClusterNodeTimeout; timeout > 0 {
				g.state.markPFailIfTimedOut(int64(timeout))
				// Promote PFAIL → FAIL when quorum of masters agrees,
				// then broadcast the FAIL so all nodes learn about it.
				for _, nodeID := range g.state.pfailNodeIDs() {
					if g.state.markNodeFail(nodeID) {
						g.broadcastMessage(msgTypeFail)
					}
				}
				// Check if we should trigger/check a failover election
				g.state.checkFailoverTick(g)
			}
		}
	}
}

func (g *gossipBus) pingOnePeer() {
	g.state.mu.RLock()
	var peers []*clusterNode
	for _, n := range g.state.nodes {
		if n.id != g.state.self.id && n.cport != 0 {
			peers = append(peers, n)
		}
	}
	g.state.mu.RUnlock()
	if len(peers) == 0 {
		return
	}
	target := peers[rand.Intn(len(peers))]
	if target == nil {
		return
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", target.ip, target.cport), time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write(g.buildMessage(msgTypePing))
	hdrBuf := make([]byte, headerLen)
	if _, err := io.ReadFull(conn, hdrBuf); err != nil {
		return
	}
	if h, err := decodeHeader(hdrBuf); err == nil {
		gossipBuf := make([]byte, int(h.gossipCount)*gossipEntLen)
		if len(gossipBuf) > 0 {
			_, _ = io.ReadFull(conn, gossipBuf)
		}
		g.mergeFromMessage(h, decodeGossip(gossipBuf, int(h.gossipCount)), target.ip)
	}
}

// handleFailMessage processes a FAIL message from a peer.
// The sender (identified by h.senderID) is reporting that a node is FAIL.
// In Redis cluster, FAIL messages carry the failed node's ID in the gossip
// section. Any entry with flagFail set should be marked FAIL locally
// (the sender has already confirmed quorum). This is how Redis propagates
// FAIL status. The actual flag adoption happens in mergeFromMessage; this
// hook exists so future work can trigger side effects (e.g. start failover).
func (g *gossipBus) handleFailMessage(h *clusterMsgHeader) {
	// Side-effect hook for FAIL propagation.
	// The gossip entries are already processed by mergeFromMessage,
	// which adopts FAIL flags from the sender's view.
}

// handleAuthRequest processes a FAILOVER_AUTH_REQUEST from a replica.
// A master grants a vote iff:
//   - reqEpoch >= currentEpoch
//   - not yet voted this epoch (lastVoteEpoch < reqEpoch)
//   - the requesting replica's master is FAIL
//
// On grant, deliver a targeted AUTH_ACK via a fresh dial to the candidate's
// cport. The ACK carries the candidate's ID in a gossip entry and the
// election epoch in configEpoch so the replica can verify the vote is for it.
func (g *gossipBus) handleAuthRequest(h *clusterMsgHeader) {
	// This node must be a master to vote
	g.state.mu.RLock()
	isMaster := g.state.self.flags&flagMaster != 0
	g.state.mu.RUnlock()

	if !isMaster {
		return
	}

	// The sender is a replica requesting a vote.
	reqEpoch := h.currentEpoch
	replicaID := h.senderID

	// Find the sender's master — check our node table
	g.state.mu.RLock()
	replica := g.state.nodes[replicaID]
	var masterID string
	if replica != nil {
		masterID = replica.masterID
	}
	g.state.mu.RUnlock()

	if masterID == "" {
		return
	}

	if g.state.grantVote(replicaID, masterID, reqEpoch) {
		// Build a targeted AUTH_ACK: senderID=voting master,
		// configEpoch=election epoch, gossip entry=candidate replica.
		g.state.mu.RLock()
		ack := clusterMsgHeader{
			msgType:      msgTypeFailoverAuthAck,
			currentEpoch: g.state.epoch,
			configEpoch:  reqEpoch,
			port:         uint32(g.state.self.port),
			cport:        uint32(g.state.self.cport),
			flags:        g.state.self.flags,
			senderID:     g.state.self.id,
			slots:        g.state.self.slots,
		}
		// Look up candidate's bus address for fresh dial
		var candidateIP string
		var candidateCport int
		if replica != nil {
			candidateIP = replica.ip
			candidateCport = replica.cport
		}
		g.state.mu.RUnlock()

		// Include the requesting replica as a gossip entry so it can
		// verify the ACK is targeted at it.
		entries := []gossipEntry{{id: replicaID}}
		body := encodeGossip(entries)
		ack.gossipCount = 1
		ack.totalLen = uint32(headerLen + len(body))
		msg := append(encodeHeader(&ack), body...)

		// Deliver via fresh dial — the candidate's broadcastMessage
		// closed the inbound connection, so conn.Write would fail.
		if candidateIP != "" && candidateCport != 0 {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", candidateIP, candidateCport), time.Second)
			if err != nil {
				return
			}
			_, _ = conn.Write(msg)
			_ = conn.Close()
		}
	}
}

// handleAuthAck processes a FAILOVER_AUTH_ACK from a master.
// The requesting replica tallies votes; on majority it can proceed.
// The ACK must carry our ID in the gossip section and our election epoch
// in configEpoch; otherwise it's a stale or misrouted vote.
func (g *gossipBus) handleAuthAck(h *clusterMsgHeader, gossipEntries []gossipEntry) {
	// We're a replica and this is a vote from a master
	g.state.mu.RLock()
	isReplica := g.state.self.flags&flagSlave != 0
	selfID := g.state.self.id
	g.state.mu.RUnlock()

	if !isReplica {
		return
	}

	// Verify the ACK is targeted at us: the gossip section must contain
	// our node ID as the candidate.
	targeted := false
	for _, e := range gossipEntries {
		if e.id == selfID {
			targeted = true
			break
		}
	}
	if !targeted {
		return
	}

	// Record the vote, passing the election epoch from configEpoch.
	_ = g.state.recordVote(h.senderID, h.configEpoch)
	// If the election is won, claimOwnership will be called on the next
	// checkFailoverTick from the pingLoop.
}

// broadcastMessage sends a message of the given type to all known peers.
func (g *gossipBus) broadcastMessage(msgType uint16) {
	g.state.mu.RLock()
	var peers []*clusterNode
	for _, n := range g.state.nodes {
		if n.id != g.state.self.id && n.cport != 0 && n.linkUp {
			peers = append(peers, n)
		}
	}
	g.state.mu.RUnlock()

	msg := g.buildMessage(msgType)
	for _, p := range peers {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", p.ip, p.cport), time.Second)
		if err != nil {
			continue
		}
		_, _ = conn.Write(msg)
		_ = conn.Close()
	}
}
