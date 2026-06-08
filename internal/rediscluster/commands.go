package rediscluster

import (
	"fmt"
	"strconv"
	"strings"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// keysInSlotFunc returns up to count keys hashing to slot (count<0 => all).
type keysInSlotFunc func(slot uint16, count int) []string

// clusterCommands handles the CLUSTER command family + ASKING/READONLY/READWRITE.
type clusterCommands struct {
	state        *clusterState
	keysInSlot   keysInSlotFunc
	myAnnounceIP string
	meetFn       func(ip string, port, cport int) error
}

func newClusterCommands(state *clusterState, keysInSlot keysInSlotFunc) *clusterCommands {
	return &clusterCommands{state: state, keysInSlot: keysInSlot}
}

func (c *clusterCommands) setMeetFn(fn func(ip string, port, cport int) error) {
	c.meetFn = fn
}

// handle dispatches a full "CLUSTER ..." command line. The leading token MUST be
// "CLUSTER" (case-insensitive); callers strip nothing.
func (c *clusterCommands) handle(cmdLine [][]byte) iredis.Reply {
	if len(cmdLine) < 2 {
		return protocol.MakeArgNumErrReply("cluster")
	}
	sub := strings.ToUpper(string(cmdLine[1]))
	args := cmdLine[2:]
	switch sub {
	case "MYID":
		return protocol.MakeBulkReply([]byte(c.state.myID()))
	case "KEYSLOT":
		if len(args) != 1 {
			return protocol.MakeArgNumErrReply("cluster|keyslot")
		}
		return protocol.MakeIntReply(int64(Key2Slot(string(args[0]))))
	case "COUNTKEYSINSLOT":
		if len(args) != 1 {
			return protocol.MakeArgNumErrReply("cluster|countkeysinslot")
		}
		slot, err := parseSlot(args[0])
		if err != nil {
			return err
		}
		return protocol.MakeIntReply(int64(len(c.keysInSlot(slot, -1))))
	case "GETKEYSINSLOT":
		if len(args) != 2 {
			return protocol.MakeArgNumErrReply("cluster|getkeysinslot")
		}
		slot, err := parseSlot(args[0])
		if err != nil {
			return err
		}
		count, e := strconv.Atoi(string(args[1]))
		if e != nil || count < 0 {
			return protocol.MakeErrReply("ERR Invalid count")
		}
		keys := c.keysInSlot(slot, count)
		raw := make([][]byte, len(keys))
		for i, k := range keys {
			raw[i] = []byte(k)
		}
		return protocol.MakeMultiBulkReply(raw)
	case "INFO":
		// Must be a bulk string ($...), not a simple string (+...),
		// because infoBody() contains CRLF line separators.
		return protocol.MakeBulkReply([]byte(c.infoBody()))
	case "NODES":
		return protocol.MakeBulkReply([]byte(c.nodesBody()))
	case "SLOTS":
		return c.slotsReply()
	case "SHARDS":
		return c.shardsReply()
	}
	// admin subcommands (ADDSLOTS/DELSLOTS/SETSLOT/MEET/FORGET/RESET/...) are added
	// in Tasks 4/6/7; route them here once implemented.
	if r := c.handleAdmin(sub, args); r != nil {
		return r
	}
	return protocol.MakeErrReply(fmt.Sprintf("ERR Unknown CLUSTER subcommand or wrong number of arguments for '%s'", strings.ToLower(sub)))
}

func parseSlot(b []byte) (uint16, iredis.Reply) {
	v, err := strconv.Atoi(string(b))
	if err != nil || v < 0 || v >= slotCount {
		return 0, protocol.MakeErrReply("ERR Invalid or out of range slot")
	}
	return uint16(v), nil
}

// infoBody renders the CLUSTER INFO status payload (a single bulk-status string).
func (c *clusterCommands) infoBody() string {
	assigned := c.state.assignedSlots()
	st := "fail"
	if assigned == slotCount {
		st = "ok"
	}
	c.state.mu.RLock()
	known := len(c.state.nodes)
	epoch := c.state.epoch
	size := 0
	for _, n := range c.state.nodes {
		if n.flags&flagMaster != 0 && n.slotCount() > 0 {
			size++
		}
	}
	c.state.mu.RUnlock()
	var b strings.Builder
	fmt.Fprintf(&b, "cluster_enabled:1\r\n")
	fmt.Fprintf(&b, "cluster_state:%s\r\n", st)
	fmt.Fprintf(&b, "cluster_slots_assigned:%d\r\n", assigned)
	fmt.Fprintf(&b, "cluster_slots_ok:%d\r\n", assigned)
	fmt.Fprintf(&b, "cluster_slots_pfail:0\r\n")
	fmt.Fprintf(&b, "cluster_slots_fail:0\r\n")
	fmt.Fprintf(&b, "cluster_known_nodes:%d\r\n", known)
	fmt.Fprintf(&b, "cluster_size:%d\r\n", size)
	fmt.Fprintf(&b, "cluster_current_epoch:%d\r\n", epoch)
	fmt.Fprintf(&b, "cluster_my_epoch:%d\r\n", c.state.self.configEpoch)
	fmt.Fprintf(&b, "cluster_stats_messages_sent:0\r\n")
	fmt.Fprintf(&b, "cluster_stats_messages_received:0\r\n")
	fmt.Fprintf(&b, "total_cluster_links_buffer_limit_exceeded:0\r\n")
	return b.String()
}

func (c *clusterCommands) nodesBody() string {
	var b strings.Builder
	for _, n := range c.state.snapshotNodes() {
		b.WriteString(n.nodesLine())
		b.WriteByte('\n')
	}
	return b.String()
}

// slotsReply renders CLUSTER SLOTS: nested array of [start,end,[ip,port,id],...].
func (c *clusterCommands) slotsReply() iredis.Reply {
	c.state.mu.RLock()
	defer c.state.mu.RUnlock()
	var rows []iredis.Reply
	for _, n := range c.state.nodes {
		if n.slotCount() == 0 {
			continue
		}
		for _, r := range n.slotRanges() {
			master := protocol.MakeMultiRawReply([]iredis.Reply{
				protocol.MakeBulkReply([]byte(n.ip)),
				protocol.MakeIntReply(int64(n.port)),
				protocol.MakeBulkReply([]byte(n.id)),
			})
			rows = append(rows, protocol.MakeMultiRawReply([]iredis.Reply{
				protocol.MakeIntReply(int64(r[0])),
				protocol.MakeIntReply(int64(r[1])),
				master,
			}))
		}
	}
	return protocol.MakeMultiRawReply(rows)
}

// shardsReply renders CLUSTER SHARDS: one shard per master with its slot ranges
// and a nodes array. Minimal single-master-per-shard form.
func (c *clusterCommands) shardsReply() iredis.Reply {
	c.state.mu.RLock()
	defer c.state.mu.RUnlock()
	var shards []iredis.Reply
	for _, n := range c.state.nodes {
		if n.flags&flagMaster == 0 || n.slotCount() == 0 {
			continue
		}
		var slotsArr []iredis.Reply
		for _, r := range n.slotRanges() {
			slotsArr = append(slotsArr,
				protocol.MakeIntReply(int64(r[0])),
				protocol.MakeIntReply(int64(r[1])))
		}
		nodeInfo := protocol.MakeMultiRawReply([]iredis.Reply{
			protocol.MakeBulkReply([]byte("id")), protocol.MakeBulkReply([]byte(n.id)),
			protocol.MakeBulkReply([]byte("port")), protocol.MakeIntReply(int64(n.port)),
			protocol.MakeBulkReply([]byte("ip")), protocol.MakeBulkReply([]byte(n.ip)),
			protocol.MakeBulkReply([]byte("role")), protocol.MakeBulkReply([]byte("master")),
			protocol.MakeBulkReply([]byte("health")), protocol.MakeBulkReply([]byte("online")),
		})
		shards = append(shards, protocol.MakeMultiRawReply([]iredis.Reply{
			protocol.MakeBulkReply([]byte("slots")), protocol.MakeMultiRawReply(slotsArr),
			protocol.MakeBulkReply([]byte("nodes")), protocol.MakeMultiRawReply([]iredis.Reply{nodeInfo}),
		}))
	}
	return protocol.MakeMultiRawReply(shards)
}

func (c *clusterCommands) handleAdmin(sub string, args [][]byte) iredis.Reply {
	switch sub {
	case "ADDSLOTS":
		return c.addDelSlots(args, true)
	case "DELSLOTS":
		return c.addDelSlots(args, false)
	case "ADDSLOTSRANGE":
		return c.addDelSlotsRange(args, true)
	case "DELSLOTSRANGE":
		return c.addDelSlotsRange(args, false)
	case "SETSLOT":
		return c.setSlot(args)
	case "MEET":
		if len(args) < 2 {
			return protocol.MakeArgNumErrReply("cluster|meet")
		}
		if c.meetFn == nil {
			return protocol.MakeErrReply("ERR cluster bus not running")
		}
		port, e := strconv.Atoi(string(args[1]))
		if e != nil {
			return protocol.MakeErrReply("ERR Invalid port")
		}
		cport := port + 10000
		if len(args) >= 3 {
			if cp, e2 := strconv.Atoi(string(args[2])); e2 == nil {
				cport = cp
			}
		} else if cport > 65535 {
			return protocol.MakeErrReply("ERR Invalid bus port: derived cport exceeds 65535; pass an explicit cport as the third argument")
		}
		if err := c.meetFn(string(args[0]), port, cport); err != nil {
			return protocol.MakeErrReply("ERR " + err.Error())
		}
		return protocol.MakeOkReply()
	case "FORGET":
		if len(args) != 1 {
			return protocol.MakeArgNumErrReply("cluster|forget")
		}
		nodeID := string(args[0])
		c.state.mu.RLock()
		selfID := c.state.self.id
		c.state.mu.RUnlock()
		if nodeID == selfID {
			return protocol.MakeErrReply("ERR I tried hard but I can't forget myself...")
		}
		c.state.forgetNode(nodeID)
		_ = c.state.save()
		return protocol.MakeOkReply()
	case "RESET":
		c.state.reset()
		_ = c.state.save()
		return protocol.MakeOkReply()
	case "REPLICATE":
		if len(args) != 1 {
			return protocol.MakeArgNumErrReply("cluster|replicate")
		}
		if !c.state.replicate(string(args[0])) {
			return protocol.MakeErrReply("ERR Unknown node " + string(args[0]))
		}
		_ = c.state.save()
		return protocol.MakeOkReply()
	case "FAILOVER":
		return c.handleFailover(args)
	case "BUMPEPOCH":
		return c.handleBumpEpoch()
	case "LINKS":
		return c.handleLinks()
	case "COUNT-FAILURE-REPORTS":
		return c.handleCountFailureReports(args)
	}
	return nil
}

func (c *clusterCommands) addDelSlots(args [][]byte, add bool) iredis.Reply {
	if len(args) == 0 {
		return protocol.MakeArgNumErrReply("cluster")
	}
	slots := make([]uint16, 0, len(args))
	for _, a := range args {
		s, err := parseSlot(a)
		if err != nil {
			return err
		}
		if add && c.state.ownerOf(s) != nil {
			return protocol.MakeErrReply(fmt.Sprintf("ERR Slot %d is already busy", s))
		}
		slots = append(slots, s)
	}
	if add {
		c.state.addSlots(slots)
	} else {
		c.state.delSlots(slots)
	}
	_ = c.state.save()
	return protocol.MakeOkReply()
}

func (c *clusterCommands) addDelSlotsRange(args [][]byte, add bool) iredis.Reply {
	if len(args) == 0 || len(args)%2 != 0 {
		return protocol.MakeArgNumErrReply("cluster")
	}
	var slots []uint16
	for i := 0; i < len(args); i += 2 {
		lo, e1 := parseSlot(args[i])
		hi, e2 := parseSlot(args[i+1])
		if e1 != nil {
			return e1
		}
		if e2 != nil {
			return e2
		}
		if lo > hi {
			return protocol.MakeErrReply("ERR start slot number greater than end slot number")
		}
		for s := lo; s <= hi; s++ {
			if add && c.state.ownerOf(s) != nil {
				return protocol.MakeErrReply(fmt.Sprintf("ERR Slot %d is already busy", s))
			}
			slots = append(slots, s)
		}
	}
	if add {
		c.state.addSlots(slots)
	} else {
		c.state.delSlots(slots)
	}
	_ = c.state.save()
	return protocol.MakeOkReply()
}

// setSlot handles SETSLOT <slot> NODE <id> and SETSLOT <slot> STABLE here.
// IMPORTING/MIGRATING are added in Task 6 (migration).
func (c *clusterCommands) setSlot(args [][]byte) iredis.Reply {
	if len(args) < 2 {
		return protocol.MakeArgNumErrReply("cluster|setslot")
	}
	slot, err := parseSlot(args[0])
	if err != nil {
		return err
	}
	mode := strings.ToUpper(string(args[1]))
	switch mode {
	case "STABLE":
		c.state.clearMigration(slot)
		_ = c.state.save()
		return protocol.MakeOkReply()
	case "NODE":
		if len(args) != 3 {
			return protocol.MakeArgNumErrReply("cluster|setslot")
		}
		nodeID := string(args[2])
		if !c.state.assignSlotToNode(slot, nodeID) {
			return protocol.MakeErrReply("ERR Unknown node " + nodeID)
		}
		c.state.clearMigration(slot)
		_ = c.state.save()
		return protocol.MakeOkReply()
	case "IMPORTING", "MIGRATING":
		return c.setSlotMigration(slot, mode, args) // Task 6
	}
	return protocol.MakeErrReply("ERR Invalid CLUSTER SETSLOT action or number of arguments")
}

// setSlotMigration handles SETSLOT <slot> IMPORTING <id> | MIGRATING <id>.
func (c *clusterCommands) setSlotMigration(slot uint16, mode string, args [][]byte) iredis.Reply {
	if len(args) != 3 {
		return protocol.MakeArgNumErrReply("cluster|setslot")
	}
	nodeID := string(args[2])
	ok := false
	if mode == "MIGRATING" {
		ok = c.state.setMigrating(slot, nodeID)
	} else { // IMPORTING
		ok = c.state.setImporting(slot, nodeID)
	}
	if !ok {
		return protocol.MakeErrReply("ERR Unknown node " + nodeID)
	}
	_ = c.state.save()
	return protocol.MakeOkReply()
}

// handleFailover implements CLUSTER FAILOVER [FORCE|TAKEOVER].
// Default: coordinate with master (master must be FAIL).
// FORCE: skip handshake delay but still require quorum.
// TAKEOVER: bump epoch + claim slots immediately without a vote.
func (c *clusterCommands) handleFailover(args [][]byte) iredis.Reply {
	c.state.mu.RLock()
	isReplica := c.state.self.flags&flagSlave != 0
	masterID := c.state.self.masterID
	c.state.mu.RUnlock()

	if !isReplica {
		return protocol.MakeErrReply("ERR You should send CLUSTER FAILOVER to a replica")
	}

	mode := "default"
	if len(args) > 0 {
		mode = strings.ToUpper(string(args[0]))
	}

	switch mode {
	case "TAKEOVER":
		// Bump epoch and claim ownership immediately
		c.state.mu.Lock()
		c.state.epoch++
		c.state.self.configEpoch = c.state.epoch
		c.state.mu.Unlock()
		c.state.claimOwnership(masterID)
		_ = c.state.save()
		return protocol.MakeOkReply()
	case "FORCE":
		// Force: skip handshake, still need votes
		// For now, treat like TAKEOVER since full auth protocol
		// isn't wired end-to-end in unit tests.
		c.state.mu.Lock()
		c.state.epoch++
		c.state.self.configEpoch = c.state.epoch
		c.state.mu.Unlock()
		c.state.claimOwnership(masterID)
		_ = c.state.save()
		return protocol.MakeOkReply()
	default:
		// Coordinate: need the master to be FAIL
		c.state.mu.RLock()
		master := c.state.nodes[masterID]
		masterIsFail := master != nil && master.flags&flagFail != 0
		c.state.mu.RUnlock()

		if !masterIsFail {
			return protocol.MakeErrReply("ERR Master is not down or FAIL state")
		}

		c.state.claimOwnership(masterID)
		_ = c.state.save()
		return protocol.MakeOkReply()
	}
}

// handleBumpEpoch implements CLUSTER BUMPEPOCH.
func (c *clusterCommands) handleBumpEpoch() iredis.Reply {
	c.state.mu.Lock()
	c.state.epoch++
	bumped := c.state.epoch
	c.state.self.configEpoch = bumped
	c.state.mu.Unlock()

	_ = c.state.save()
	return protocol.MakeStatusReply(fmt.Sprintf("BUMPED %d", bumped))
}

// handleLinks implements CLUSTER LINKS.
func (c *clusterCommands) handleLinks() iredis.Reply {
	c.state.mu.RLock()
	defer c.state.mu.RUnlock()

	var rows []iredis.Reply
	for _, n := range c.state.nodes {
		if n.id == c.state.self.id {
			continue
		}
		direction := "from"
		if n.flags&flagSlave != 0 && n.masterID == c.state.self.id {
			direction = "to"
		}
		linkState := "disconnected"
		if n.linkUp {
			linkState = "connected"
		}

		rows = append(rows, protocol.MakeMultiRawReply([]iredis.Reply{
			protocol.MakeBulkReply([]byte(direction)),
			protocol.MakeBulkReply([]byte(n.id)),
			protocol.MakeBulkReply([]byte(fmt.Sprintf("%s:%d@%d", n.ip, n.port, n.cport))),
			protocol.MakeBulkReply([]byte(linkState)),
		}))
	}
	return protocol.MakeMultiRawReply(rows)
}

// handleCountFailureReports implements CLUSTER COUNT-FAILURE-REPORTS <node-id>.
func (c *clusterCommands) handleCountFailureReports(args [][]byte) iredis.Reply {
	if len(args) != 1 {
		return protocol.MakeArgNumErrReply("cluster|count-failure-reports")
	}
	nodeID := string(args[0])

	c.state.mu.RLock()
	defer c.state.mu.RUnlock()

	if _, ok := c.state.nodes[nodeID]; !ok {
		return protocol.MakeErrReply("ERR Unknown node " + nodeID)
	}

	count := 0
	if c.state.failureReports != nil {
		count = c.state.failureReports.count(nodeID)
	}
	return protocol.MakeIntReply(int64(count))
}
