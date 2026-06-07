package rediscluster

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// migration tracks a slot being moved in/out of this node.
type migration struct {
	importingFrom string // node id we are importing this slot FROM (we are target)
	migratingTo   string // node id we are migrating this slot TO (we are source)
}

// clusterState is the authoritative slot map + node set for THIS node.
// It is read on every command (redirection hot path) and mutated by CLUSTER
// admin commands and the gossip bus, so it carries its own RWMutex.
type clusterState struct {
	mu         sync.RWMutex
	self       *clusterNode
	nodes      map[string]*clusterNode // id -> node (includes self)
	slots      [slotCount]*clusterNode // slot -> owning node (nil = unassigned)
	migrations map[uint16]*migration   // slot -> migration state
	epoch      uint64                  // current epoch
	confPath   string
}

func newClusterState(ip string, port int, confPath string) *clusterState {
	self := newNode(genNodeID(), ip, port)
	self.flags |= flagMyself
	return &clusterState{
		self:       self,
		nodes:      map[string]*clusterNode{self.id: self},
		migrations: map[uint16]*migration{},
		confPath:   confPath,
	}
}

func (s *clusterState) myID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.id
}

func (s *clusterState) ownerOf(slot uint16) *clusterNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.slots[slot]
}

func (s *clusterState) imOwner(slot uint16) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.slots[slot] == s.self
}

func (s *clusterState) addSlots(slots []uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range slots {
		s.slots[sl] = s.self
		s.self.addSlot(sl)
	}
}

func (s *clusterState) delSlots(slots []uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range slots {
		if s.slots[sl] == s.self {
			s.slots[sl] = nil
		}
		s.self.delSlot(sl)
	}
}

// assignSlotToNode points slot at the node with id; returns false if unknown.
func (s *clusterState) assignSlotToNode(slot uint16, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodes[id]
	if n == nil {
		return false
	}
	if old := s.slots[slot]; old != nil {
		old.delSlot(slot)
	}
	s.slots[slot] = n
	n.addSlot(slot)
	return true
}

func (s *clusterState) clearMigration(slot uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.migrations, slot)
}

func (s *clusterState) assignedSlots() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := 0
	for _, n := range s.slots {
		if n != nil {
			c++
		}
	}
	return c
}

// stateOK reports cluster_state: ok|fail (ok iff all 16384 slots are assigned).
func (s *clusterState) stateOK() bool { return s.assignedSlots() == slotCount }

// snapshotNodes returns a stable slice of nodes for read-only rendering.
func (s *clusterState) snapshotNodes() []*clusterNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*clusterNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, n)
	}
	return out
}

// save writes nodes.conf in Redis's format: one node line per node, then a
// trailing "vars currentEpoch <n> lastVoteEpoch 0" line.
func (s *clusterState) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var b strings.Builder
	for _, n := range s.nodes {
		b.WriteString(n.nodesLine())
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "vars currentEpoch %d lastVoteEpoch 0\n", s.epoch)
	tmp := s.confPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.confPath)
}

// load reads nodes.conf back, restoring self (the "myself" line) and peers.
func (s *clusterState) load() error {
	f, err := os.Open(s.confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // first boot: keep the freshly-generated self
		}
		return err
	}
	defer f.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes = map[string]*clusterNode{}
	for i := range s.slots {
		s.slots[i] = nil
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "vars ") {
			fields := strings.Fields(line)
			for i := 0; i+1 < len(fields); i += 2 {
				if fields[i] == "currentEpoch" {
					s.epoch, _ = strconv.ParseUint(fields[i+1], 10, 64)
				}
			}
			continue
		}
		n, slots, isSelf := parseNodeLine(line)
		if n == nil {
			continue
		}
		s.nodes[n.id] = n
		if isSelf {
			s.self = n
		}
		for _, sl := range slots {
			s.slots[sl] = n
			n.addSlot(sl)
		}
	}
	if s.self == nil {
		return fmt.Errorf("nodes.conf %s has no myself line", s.confPath)
	}
	return scanner.Err()
}

// parseNodeLine parses one CLUSTER NODES / nodes.conf line. Returns the node, the
// flat list of owned slots, and whether the node is "myself".
func parseNodeLine(line string) (*clusterNode, []uint16, bool) {
	f := strings.Fields(line)
	if len(f) < 8 {
		return nil, nil, false
	}
	n := &clusterNode{id: f[0]}
	// f[1] = ip:port@cport
	addr := f[1]
	if at := strings.IndexByte(addr, '@'); at >= 0 {
		n.cport, _ = strconv.Atoi(addr[at+1:])
		addr = addr[:at]
	}
	if c := strings.LastIndexByte(addr, ':'); c >= 0 {
		n.ip = addr[:c]
		n.port, _ = strconv.Atoi(addr[c+1:])
	}
	isSelf := false
	for _, fl := range strings.Split(f[2], ",") {
		switch fl {
		case "myself":
			n.flags |= flagMyself
			isSelf = true
		case "master":
			n.flags |= flagMaster
		case "slave":
			n.flags |= flagSlave
		case "fail?":
			n.flags |= flagPFail
		case "fail":
			n.flags |= flagFail
		case "handshake":
			n.flags |= flagHandshake
		case "noaddr":
			n.flags |= flagNoAddr
		}
	}
	if f[3] != "-" {
		n.masterID = f[3]
	}
	n.pingSent, _ = strconv.ParseInt(f[4], 10, 64)
	n.pongRecv, _ = strconv.ParseInt(f[5], 10, 64)
	n.configEpoch, _ = strconv.ParseUint(f[6], 10, 64)
	n.linkUp = f[7] == "connected"
	var slots []uint16
	for _, tok := range f[8:] {
		if strings.HasPrefix(tok, "[") {
			continue // migration annotation, e.g. [12-<-nodeid]; ignored on load
		}
		if dash := strings.IndexByte(tok, '-'); dash >= 0 {
			lo, _ := strconv.Atoi(tok[:dash])
			hi, _ := strconv.Atoi(tok[dash+1:])
			for s := lo; s <= hi; s++ {
				slots = append(slots, uint16(s))
			}
		} else {
			s, _ := strconv.Atoi(tok)
			slots = append(slots, uint16(s))
		}
	}
	return n, slots, isSelf
}
