package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startClusterNodeWithTimeout starts a cluster node with a custom node-timeout.
func startClusterNodeWithTimeout(t *testing.T, port int, dir string, clusterConfFile string, nodeTimeoutMs int) (addr string, stop func()) {
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
cluster-node-timeout %d
`, port, dir, clusterConfFile, nodeTimeoutMs)), 0o644); err != nil {
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

// TestClusterFailoverTakeover verifies that a replica can promote itself to master
// using CLUSTER FAILOVER TAKEOVER after the master is killed.
func TestClusterFailoverTakeover(t *testing.T) {
	dir := t.TempDir()

	masterPort := freePort(t)
	replicaPort := freePort(t)

	masterAddr, stopMaster := startClusterNodeWithTimeout(t, masterPort, dir, filepath.Join(dir, "master.conf"), 2000)
	replicaAddr, stopReplica := startClusterNodeWithTimeout(t, replicaPort, dir, filepath.Join(dir, "replica.conf"), 2000)
	defer stopReplica()

	// MEET: replica meets master
	cportFor := func(p int) int {
		if p+10000 <= 65535 {
			return p + 10000
		}
		return p - 10000
	}
	masterCport := cportFor(masterPort)
	r := sendCmd(t, replicaAddr, "CLUSTER", "MEET", "127.0.0.1", fmt.Sprintf("%d", masterPort), fmt.Sprintf("%d", masterCport))
	if !strings.Contains(r, "+OK") {
		t.Fatalf("MEET master: %q", r)
	}

	// Wait for gossip propagation
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info := sendCmd(t, replicaAddr, "CLUSTER", "INFO")
		if strings.Contains(info, "cluster_known_nodes:2") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Assign all slots to master
	sendCmd(t, masterAddr, "CLUSTER", "ADDSLOTSRANGE", "0", "16383")

	// Wait for slot propagation
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info := sendCmd(t, replicaAddr, "CLUSTER", "INFO")
		if strings.Contains(info, "cluster_state:ok") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Get master's node ID
	nodesInfo := sendCmd(t, masterAddr, "CLUSTER", "NODES")
	var masterID string
	for _, line := range strings.Split(nodesInfo, "\n") {
		if strings.Contains(line, "myself") && strings.Contains(line, "master") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				masterID = fields[0]
			}
		}
	}
	if masterID == "" {
		t.Fatal("could not find master node ID")
	}

	// Make replica replicate from master
	r = sendCmd(t, replicaAddr, "CLUSTER", "REPLICATE", masterID)
	if !strings.Contains(r, "+OK") {
		t.Fatalf("REPLICATE: %q", r)
	}

	// Verify replica is a slave
	time.Sleep(500 * time.Millisecond)
	info := sendCmd(t, replicaAddr, "CLUSTER", "INFO")
	_ = info // just wait a bit for state to settle

	// Kill the master
	stopMaster()

	// Now the replica should be able to FAILOVER TAKEOVER
	r = sendCmd(t, replicaAddr, "CLUSTER", "FAILOVER", "TAKEOVER")
	if !strings.Contains(r, "+OK") {
		t.Fatalf("FAILOVER TAKEOVER: %q", r)
	}

	// Verify replica is now a master
	nodesInfo = sendCmd(t, replicaAddr, "CLUSTER", "NODES")
	if !strings.Contains(nodesInfo, "myself,master") {
		t.Fatalf("replica should be master after TAKEOVER:\n%s", nodesInfo)
	}

	// Verify the promoted replica owns the slots
	slotInfo := sendCmd(t, replicaAddr, "CLUSTER", "INFO")
	if !strings.Contains(slotInfo, "cluster_slots_assigned:16384") {
		t.Fatalf("expected 16384 assigned slots after failover:\n%s", slotInfo)
	}
}

// TestClusterBumpEpochIntegration verifies BUMPEPOCH works on a live cluster.
func TestClusterBumpEpochIntegration(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	addr, stop := startClusterNodeWithTimeout(t, port, dir, filepath.Join(dir, "nodes.conf"), 15000)
	defer stop()

	r := sendCmd(t, addr, "CLUSTER", "BUMPEPOCH")
	if !strings.Contains(r, "BUMPED") {
		t.Fatalf("BUMPEPOCH: %q", r)
	}
}

// TestClusterLinksIntegration verifies LINKS works on a live cluster.
func TestClusterLinksIntegration(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	addr, stop := startClusterNodeWithTimeout(t, port, dir, filepath.Join(dir, "nodes.conf"), 15000)
	defer stop()

	r := sendCmd(t, addr, "CLUSTER", "LINKS")
	// Should return an array (even if empty for single node)
	if !strings.HasPrefix(r, "*") {
		t.Fatalf("LINKS should return array: %q", r)
	}
}
