package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
