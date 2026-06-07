package rediscluster

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
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
//   id[40] ip[46] port[4] cport[4] flags[4]
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
	g.mergeFromMessage(h, decodeGossip(gossipBuf, int(h.gossipCount)))

	// Reply to PING/MEET with a PONG carrying our own view.
	if h.msgType == msgTypePing || h.msgType == msgTypeMeet {
		pong := g.buildMessage(msgTypePong)
		_, _ = conn.Write(pong)
	}
}

// mergeFromMessage updates local state with the sender's identity + slots and any
// gossiped peers it didn't already know.
func (g *gossipBus) mergeFromMessage(h *clusterMsgHeader, entries []gossipEntry) {
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
		// Adopt the sender's slot ownership (authoritative for slots it claims).
		for s := uint16(0); s < slotCount; s++ {
			owns := h.slots[s/8]&(1<<(s%8)) != 0
			if owns {
				if old := g.state.slots[s]; old != nil && old != sender {
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
		if _, ok := g.state.nodes[e.id]; !ok {
			g.state.nodes[e.id] = &clusterNode{
				id: e.id, ip: e.ip, port: int(e.port), cport: int(e.cport),
				flags: e.flags, linkUp: true,
			}
		}
	}
}

// buildMessage assembles a header + gossip section reflecting our current view.
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
	var entries []gossipEntry
	for _, n := range g.state.nodes {
		if n.id == g.state.self.id {
			continue
		}
		entries = append(entries, gossipEntry{
			id: n.id, ip: n.ip, port: uint32(n.port), cport: uint32(n.cport), flags: n.flags,
		})
		if len(entries) >= 3 { // small sample, like redis
			break
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
	g.mergeFromMessage(h, decodeGossip(gossipBuf, int(h.gossipCount)))
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
		}
	}
}

func (g *gossipBus) pingOnePeer() {
	g.state.mu.RLock()
	var target *clusterNode
	for _, n := range g.state.nodes {
		if n.id != g.state.self.id && n.cport != 0 {
			target = n
			break
		}
	}
	g.state.mu.RUnlock()
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
		g.mergeFromMessage(h, decodeGossip(gossipBuf, int(h.gossipCount)))
	}
}
