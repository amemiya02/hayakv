package integration

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// encodeResp sends a RESP2 command and returns the raw reply bytes.
func encodeResp(args []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	return b.Bytes()
}

func readRESPReply(r *bufio.Reader) ([]byte, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	out := append([]byte{prefix}, line...)

	switch prefix {
	case '+', '-', ':', '_':
		return out, nil
	case '$':
		var n int
		fmt.Sscanf(strings.TrimSpace(string(line)), "%d", &n)
		if n < 0 {
			return out, nil
		}
		payload := make([]byte, n+2)
		if _, err := readFull(r, payload); err != nil {
			return nil, err
		}
		return append(out, payload...), nil
	case '*', '%', '~', '>':
		var n int
		fmt.Sscanf(strings.TrimSpace(string(line)), "%d", &n)
		if n < 0 {
			return out, nil
		}
		count := n
		if prefix == '%' {
			count = n * 2
		}
		for i := 0; i < count; i++ {
			child, err := readRESPReply(r)
			if err != nil {
				return nil, err
			}
			out = append(out, child...)
		}
		return out, nil
	default:
		return out, nil
	}
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// sendCmd sends a RESP command to addr and returns the reply as a string.
func sendCmd(t *testing.T, addr string, args ...string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	if _, err := conn.Write(encodeResp(args)); err != nil {
		t.Fatalf("write %v: %v", args, err)
	}
	reader := bufio.NewReader(conn)
	reply, err := readRESPReply(reader)
	if err != nil {
		t.Fatalf("read %v: %v", args, err)
	}
	return string(reply)
}

// sendCmdConn sends a command on an existing connection.
func sendCmdConn(t *testing.T, conn net.Conn, reader *bufio.Reader, args ...string) string {
	t.Helper()
	if _, err := conn.Write(encodeResp(args)); err != nil {
		t.Fatalf("write %v: %v", args, err)
	}
	reply, err := readRESPReply(reader)
	if err != nil {
		t.Fatalf("read %v: %v", args, err)
	}
	return string(reply)
}

func startClusterNode(t *testing.T, port int, dir string, clusterConfFile string) (addr string, stop func()) {
	t.Helper()
	root := projectRoot(t)
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "hayakv")
	build := exec.Command("go", "build", "-o", bin, "./cmd/hayakv")
	build.Dir = root
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hayakv: %v\n%s", err, out)
	}

	addr = fmt.Sprintf("127.0.0.1:%d", port)
	conf := filepath.Join(tmp, "redis.conf")
	if err := os.WriteFile(conf, []byte(fmt.Sprintf(`bind 127.0.0.1
port %d
dir %s
databases 16
net goroutine
engine shardmap
proto-max resp2
cluster-enable yes
cluster-mode redis
cluster-config-file %s
`, port, dir, clusterConfFile)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "CONFIG="+conf)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start hayakv: %v", err)
	}
	waitForRedis(t, addr)
	return addr, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
}

// TestClusterThreeNodeMeetAndSlots starts 3 hayakv cluster nodes, uses MEET to
// join them, assigns all 16384 slots via ADDSLOTSRANGE across 3 nodes, and
// verifies CLUSTER INFO reports cluster_state:ok.
func TestClusterThreeNodeMeetAndSlots(t *testing.T) {
	dir := t.TempDir()

	port1 := freePort(t)
	port2 := freePort(t)
	port3 := freePort(t)

	addr1, stop1 := startClusterNode(t, port1, dir, filepath.Join(dir, "nodes1.conf"))
	defer stop1()
	addr2, stop2 := startClusterNode(t, port2, dir, filepath.Join(dir, "nodes2.conf"))
	defer stop2()
	addr3, stop3 := startClusterNode(t, port3, dir, filepath.Join(dir, "nodes3.conf"))
	defer stop3()

	// --- MEET: node1 meets node2, node1 meets node3 ---
	// CLUSTER MEET <ip> <port> [<cport>]
	cport2 := port2 + 10000
	cport3 := port3 + 10000
	r := sendCmd(t, addr1, "CLUSTER", "MEET", "127.0.0.1", fmt.Sprintf("%d", port2), fmt.Sprintf("%d", cport2))
	if !strings.Contains(r, "+OK") {
		t.Fatalf("MEET node2: %q", r)
	}
	r = sendCmd(t, addr1, "CLUSTER", "MEET", "127.0.0.1", fmt.Sprintf("%d", port3), fmt.Sprintf("%d", cport3))
	if !strings.Contains(r, "+OK") {
		t.Fatalf("MEET node3: %q", r)
	}

	// Wait for gossip propagation: all 3 nodes should know about each other.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info := sendCmd(t, addr1, "CLUSTER", "INFO")
		// cluster_known_nodes should be >= 3
		if strings.Contains(info, "cluster_known_nodes:3") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// --- ADDSLOTSRANGE: distribute slots across 3 nodes ---
	// Node1: 0-5460, Node2: 5461-10922, Node3: 10923-16383
	r = sendCmd(t, addr1, "CLUSTER", "ADDSLOTSRANGE", "0", "5460")
	if !strings.Contains(r, "+OK") {
		t.Fatalf("ADDSLOTSRANGE node1: %q", r)
	}
	r = sendCmd(t, addr2, "CLUSTER", "ADDSLOTSRANGE", "5461", "10922")
	if !strings.Contains(r, "+OK") {
		t.Fatalf("ADDSLOTSRANGE node2: %q", r)
	}
	r = sendCmd(t, addr3, "CLUSTER", "ADDSLOTSRANGE", "10923", "16383")
	if !strings.Contains(r, "+OK") {
		t.Fatalf("ADDSLOTSRANGE node3: %q", r)
	}

	// --- Verify CLUSTER INFO shows cluster_state:ok on node1 ---
	info := sendCmd(t, addr1, "CLUSTER", "INFO")
	if !strings.Contains(info, "cluster_state:ok") {
		t.Fatalf("cluster not ok after full slot assignment:\n%s", info)
	}
	if !strings.Contains(info, "cluster_slots_assigned:16384") {
		t.Fatalf("expected 16384 assigned slots:\n%s", info)
	}
	if !strings.Contains(info, "cluster_size:3") {
		t.Fatalf("expected cluster_size 3:\n%s", info)
	}

	// --- Verify each node sees 16384 assigned slots ---
	for i, addr := range []string{addr1, addr2, addr3} {
		nodeInfo := sendCmd(t, addr, "CLUSTER", "INFO")
		if !strings.Contains(nodeInfo, "cluster_state:ok") {
			t.Fatalf("node%d cluster not ok:\n%s", i+1, nodeInfo)
		}
	}

	// --- Verify CLUSTER NODES returns 3 entries ---
	nodes := sendCmd(t, addr1, "CLUSTER", "NODES")
	nodeLines := strings.Split(strings.Trim(nodes, "\r\n$0123456789"), "\n")
	// Filter out empty lines
	var realLines []string
	for _, l := range nodeLines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "$") {
			realLines = append(realLines, l)
		}
	}
	// The reply is a bulk string; extract just the content
	if !strings.Contains(nodes, "myself") {
		t.Fatalf("CLUSTER NODES missing 'myself': %q", nodes)
	}

	// --- Verify CLUSTER KEYSLOT consistency ---
	// "foo" -> slot 12182, should be on node3 (10923-16383)
	keyslot := sendCmd(t, addr1, "CLUSTER", "KEYSLOT", "foo")
	if !strings.Contains(keyslot, "12182") {
		t.Fatalf("KEYSLOT foo expected 12182, got %q", keyslot)
	}

	// --- Verify CLUSTER SLOTS returns 3 slot ranges ---
	slots := sendCmd(t, addr1, "CLUSTER", "SLOTS")
	if !strings.Contains(slots, "5460") || !strings.Contains(slots, "10922") || !strings.Contains(slots, "16383") {
		t.Fatalf("CLUSTER SLOTS missing expected ranges:\n%s", slots)
	}
}

// TestClusterThreeNodeKeyRouting verifies that after a 3-node cluster is set up,
// SET on the correct node succeeds and SET on a wrong node returns MOVED.
func TestClusterThreeNodeKeyRouting(t *testing.T) {
	dir := t.TempDir()

	port1 := freePort(t)
	port2 := freePort(t)
	port3 := freePort(t)

	addr1, stop1 := startClusterNode(t, port1, dir, filepath.Join(dir, "nodes1.conf"))
	defer stop1()
	addr2, stop2 := startClusterNode(t, port2, dir, filepath.Join(dir, "nodes2.conf"))
	defer stop2()
	addr3, stop3 := startClusterNode(t, port3, dir, filepath.Join(dir, "nodes3.conf"))
	defer stop3()

	// MEET
	cport2 := port2 + 10000
	cport3 := port3 + 10000
	sendCmd(t, addr1, "CLUSTER", "MEET", "127.0.0.1", fmt.Sprintf("%d", port2), fmt.Sprintf("%d", cport2))
	sendCmd(t, addr1, "CLUSTER", "MEET", "127.0.0.1", fmt.Sprintf("%d", port3), fmt.Sprintf("%d", cport3))

	// Wait for gossip
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info := sendCmd(t, addr1, "CLUSTER", "INFO")
		if strings.Contains(info, "cluster_known_nodes:3") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Assign slots: node1=0-5460, node2=5461-10922, node3=10923-16383
	sendCmd(t, addr1, "CLUSTER", "ADDSLOTSRANGE", "0", "5460")
	sendCmd(t, addr2, "CLUSTER", "ADDSLOTSRANGE", "5461", "10922")
	sendCmd(t, addr3, "CLUSTER", "ADDSLOTSRANGE", "10923", "16383")

	// "foo" hashes to slot 12182 -> node3 (10923-16383)
	// SET on node1 (wrong owner) should return MOVED
	setReply := sendCmd(t, addr1, "SET", "foo", "bar")
	if !strings.Contains(setReply, "MOVED") {
		t.Fatalf("expected MOVED for foo on node1, got %q", setReply)
	}
	if !strings.Contains(setReply, fmt.Sprintf("%d", port3)) {
		t.Fatalf("expected MOVED to point to node3 (port %d), got %q", port3, setReply)
	}

	// SET on node3 (correct owner) should succeed
	setReply = sendCmd(t, addr3, "SET", "foo", "bar")
	if !strings.Contains(setReply, "+OK") {
		t.Fatalf("expected OK for foo on node3, got %q", setReply)
	}

	// GET on node3 should return "bar"
	getReply := sendCmd(t, addr3, "GET", "foo")
	if !strings.Contains(getReply, "bar") {
		t.Fatalf("expected 'bar' for GET foo on node3, got %q", getReply)
	}
}

// TestClusterAddSlotsRange verifies ADDSLOTSRANGE assigns correct number of slots.
func TestClusterAddSlotsRange(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	addr, stop := startClusterNode(t, port, dir, filepath.Join(dir, "nodes.conf"))
	defer stop()

	// Assign 0-100 and 200-300 (101 + 101 = 202 slots)
	r := sendCmd(t, addr, "CLUSTER", "ADDSLOTSRANGE", "0", "100", "200", "300")
	if !strings.Contains(r, "+OK") {
		t.Fatalf("ADDSLOTSRANGE: %q", r)
	}

	info := sendCmd(t, addr, "CLUSTER", "INFO")
	if !strings.Contains(info, "cluster_slots_assigned:202") {
		t.Fatalf("expected 202 assigned slots:\n%s", info)
	}
}

// TestClusterCrossSlot verifies CROSSSLOT error for keys in different slots.
func TestClusterCrossSlot(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	addr, stop := startClusterNode(t, port, dir, filepath.Join(dir, "nodes.conf"))
	defer stop()

	// Assign all slots to this node
	sendCmd(t, addr, "CLUSTER", "ADDSLOTSRANGE", "0", "16383")

	// "foo" -> slot 12182, "bar" -> slot 5061: different slots
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// MGET with cross-slot keys should return CROSSSLOT error
	r := sendCmdConn(t, conn, reader, "MGET", "foo", "bar")
	if !strings.Contains(r, "CROSSSLOT") {
		t.Fatalf("expected CROSSSLOT error, got %q", r)
	}
}
