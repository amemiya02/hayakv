package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// startHayakvRepl starts a hayakv server with the given extra config lines and
// returns its addr and a stop func. AOF is enabled because replication RDB/AOF
// generation requires a persister.
func startHayakvRepl(t *testing.T, extra string) (addr string, stop func()) {
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
	port := freePort(t)
	addr = fmt.Sprintf("127.0.0.1:%d", port)
	conf := filepath.Join(tmp, "redis.conf")
	body := fmt.Sprintf(`bind 127.0.0.1
port %d
dir %s
databases 16
net goroutine
engine shardmap
proto-max resp2
appendonly yes
appendfilename appendonly.aof
appendfsync everysec
%s
`, port, tmp, extra)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
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

// pollGet retries GET key on c until it equals want or the deadline passes.
func pollGet(t *testing.T, c *redis.Client, key, want string, within time.Duration) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(within)
	var last string
	for time.Now().Before(deadline) {
		last = c.Get(ctx, key).Val()
		if last == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("GET %s = %q after %s, want %q", key, last, within, want)
}

func TestReplicaofAlias(t *testing.T) {
	masterAddr, stopMaster := startHayakvRepl(t, "")
	defer stopMaster()
	replicaAddr, stopReplica := startHayakvRepl(t, "")
	defer stopReplica()

	ctx := context.Background()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	replica := redis.NewClient(&redis.Options{Addr: replicaAddr, Protocol: 2})
	defer replica.Close()

	if err := master.Set(ctx, "k1", "v1", 0).Err(); err != nil {
		t.Fatalf("master SET: %v", err)
	}
	// Use REPLICAOF (the M7 alias), not SLAVEOF.
	mhost, mport := splitAddr(t, masterAddr)
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("REPLICAOF: %v", err)
	}
	pollGet(t, replica, "k1", "v1", 10*time.Second)
}

func splitAddr(t *testing.T, addr string) (host, port string) {
	t.Helper()
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:]
		}
	}
	t.Fatalf("bad addr %q", addr)
	return "", ""
}

func TestReplconfGetackUpdatesMasterView(t *testing.T) {
	masterAddr, stopMaster := startHayakvRepl(t, "")
	defer stopMaster()
	replicaAddr, stopReplica := startHayakvRepl(t, "")
	defer stopReplica()

	ctx := context.Background()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	replica := redis.NewClient(&redis.Options{Addr: replicaAddr, Protocol: 2})
	defer replica.Close()

	mhost, mport := splitAddr(t, masterAddr)
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("REPLICAOF: %v", err)
	}
	// Let the replica attach and go online.
	if err := master.Set(ctx, "warm", "up", 0).Err(); err != nil {
		t.Fatalf("SET warm: %v", err)
	}
	pollGet(t, replica, "warm", "up", 10*time.Second)

	// Write more, then poll INFO replication on the master until a slave0 line
	// reports a positive offset (proves GETACK/ACK flow updates the master view).
	for i := 0; i < 20; i++ {
		_ = master.Set(ctx, fmt.Sprintf("k%d", i), "v", 0).Err()
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info := master.Info(ctx, "replication").Val()
		if strings.Contains(info, "slave0:") && strings.Contains(info, "state=online") &&
			!strings.Contains(info, "offset=0,") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("master never saw a slave0 with non-zero offset:\n%s", master.Info(ctx, "replication").Val())
}

func TestWaitOneReplica(t *testing.T) {
	masterAddr, stopMaster := startHayakvRepl(t, "")
	defer stopMaster()
	replicaAddr, stopReplica := startHayakvRepl(t, "")
	defer stopReplica()

	ctx := context.Background()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	replica := redis.NewClient(&redis.Options{Addr: replicaAddr, Protocol: 2})
	defer replica.Close()

	mhost, mport := splitAddr(t, masterAddr)
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("REPLICAOF: %v", err)
	}
	if err := master.Set(ctx, "wk", "wv", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	pollGet(t, replica, "wk", "wv", 10*time.Second)

	// WAIT 1 1000 should return 1 (one replica has caught up within 1s).
	n, err := master.Do(ctx, "WAIT", 1, 1000).Int64()
	if err != nil {
		t.Fatalf("WAIT: %v", err)
	}
	if n < 1 {
		t.Fatalf("WAIT 1 1000 = %d, want >= 1", n)
	}
}

func TestDisklessFullResync(t *testing.T) {
	// Master uses diskless replication.
	masterAddr, stopMaster := startHayakvRepl(t, "repl-diskless-sync yes")
	defer stopMaster()
	replicaAddr, stopReplica := startHayakvRepl(t, "")
	defer stopReplica()

	ctx := context.Background()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	replica := redis.NewClient(&redis.Options{Addr: replicaAddr, Protocol: 2})
	defer replica.Close()

	// Seed multiple keys so the RDB is non-trivial before the replica attaches.
	for i := 0; i < 50; i++ {
		if err := master.Set(ctx, fmt.Sprintf("dk%d", i), fmt.Sprintf("dv%d", i), 0).Err(); err != nil {
			t.Fatalf("seed SET: %v", err)
		}
	}
	mhost, mport := splitAddr(t, masterAddr)
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("REPLICAOF: %v", err)
	}
	// All 50 seeded keys must arrive via the diskless RDB.
	pollGet(t, replica, "dk0", "dv0", 15*time.Second)
	pollGet(t, replica, "dk49", "dv49", 15*time.Second)

	// And live propagation after the diskless full sync.
	if err := master.Set(ctx, "live", "after", 0).Err(); err != nil {
		t.Fatalf("live SET: %v", err)
	}
	pollGet(t, replica, "live", "after", 10*time.Second)
}

func TestPartialResyncAfterBriefDisconnect(t *testing.T) {
	masterAddr, stopMaster := startHayakvRepl(t, "repl-backlog-size 1048576")
	defer stopMaster()
	replicaAddr, stopReplica := startHayakvRepl(t, "")
	defer stopReplica()

	ctx := context.Background()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	replica := redis.NewClient(&redis.Options{Addr: replicaAddr, Protocol: 2})
	defer replica.Close()

	mhost, mport := splitAddr(t, masterAddr)
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("REPLICAOF: %v", err)
	}
	if err := master.Set(ctx, "base", "0", 0).Err(); err != nil {
		t.Fatalf("SET base: %v", err)
	}
	pollGet(t, replica, "base", "0", 10*time.Second)

	replidBefore := extractInfoField(t, master, "master_replid:")

	// Briefly detach and re-attach the replica. Re-issuing REPLICAOF to the same
	// master triggers a reconnect; the replica retains replId/replOffset
	// and the backlog still covers it, so data stays consistent.
	if err := replica.Do(ctx, "REPLICAOF", "NO", "ONE").Err(); err != nil {
		t.Fatalf("REPLICAOF NO ONE: %v", err)
	}
	// Write within the backlog window while detached.
	if err := master.Set(ctx, "during", "1", 0).Err(); err != nil {
		t.Fatalf("SET during: %v", err)
	}
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("re-REPLICAOF: %v", err)
	}
	pollGet(t, replica, "during", "1", 10*time.Second)

	// More live writes flow after resync.
	if err := master.Set(ctx, "after", "2", 0).Err(); err != nil {
		t.Fatalf("SET after: %v", err)
	}
	pollGet(t, replica, "after", "2", 10*time.Second)

	// The master's replId is stable (no full-failover reset).
	replidAfter := extractInfoField(t, master, "master_replid:")
	if replidBefore != replidAfter {
		t.Fatalf("master_replid changed %q -> %q (unexpected reset)", replidBefore, replidAfter)
	}
}

func TestReplicaReconnectStaysConsistent(t *testing.T) {
	masterAddr, stopMaster := startHayakvRepl(t, "repl-backlog-size 1048576")
	defer stopMaster()
	// Short repl-timeout so slaveCron reconnects quickly if the link stalls.
	replicaAddr, stopReplica := startHayakvRepl(t, "repl-timeout 1")
	defer stopReplica()

	ctx := context.Background()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	replica := redis.NewClient(&redis.Options{Addr: replicaAddr, Protocol: 2})
	defer replica.Close()

	mhost, mport := splitAddr(t, masterAddr)
	if err := replica.Do(ctx, "REPLICAOF", mhost, mport).Err(); err != nil {
		t.Fatalf("REPLICAOF: %v", err)
	}
	// Continuous writes across at least one 10s cron tick + reconnect cycles.
	for i := 0; i < 200; i++ {
		if err := master.Set(ctx, fmt.Sprintf("c%d", i), fmt.Sprintf("%d", i), 0).Err(); err != nil {
			t.Fatalf("SET c%d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	pollGet(t, replica, "c0", "0", 15*time.Second)
	pollGet(t, replica, "c199", "199", 15*time.Second)
}

func extractInfoField(t *testing.T, c *redis.Client, prefix string) string {
	t.Helper()
	info := c.Info(context.Background(), "replication").Val()
	i := strings.Index(info, prefix)
	if i < 0 {
		t.Fatalf("INFO replication missing %q:\n%s", prefix, info)
	}
	rest := info[i+len(prefix):]
	if j := strings.IndexAny(rest, "\r\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestWaitTimesOutWhenNotEnoughReplicas(t *testing.T) {
	masterAddr, stopMaster := startHayakvRepl(t, "")
	defer stopMaster()
	master := redis.NewClient(&redis.Options{Addr: masterAddr, Protocol: 2})
	defer master.Close()
	ctx := context.Background()
	if err := master.Set(ctx, "x", "y", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	start := time.Now()
	n, err := master.Do(ctx, "WAIT", 2, 300).Int64() // no replicas; must time out at ~300ms
	if err != nil {
		t.Fatalf("WAIT: %v", err)
	}
	if n != 0 {
		t.Fatalf("WAIT 2 300 = %d, want 0", n)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("WAIT returned too early: %s", elapsed)
	}
}
