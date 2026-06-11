package diff

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// freeClusterPort returns a free TCP port at most 55535, as required by
// cluster mode: the gossip bus listens on port+10000, which must stay within
// the uint16 range. The OS ephemeral range used by freePort is too high.
func freeClusterPort(t *testing.T) int {
	t.Helper()
	for p := 16400; p <= 55000; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			_ = ln.Close()
			return p
		}
	}
	t.Fatalf("no free cluster port found in [16400,55000]")
	return 0
}

// startHayakvCluster starts a cluster-mode hayakv (cluster-enable yes,
// cluster-mode redis) so the CLUSTER command family is registered.
func startHayakvCluster(t *testing.T) (string, func()) {
	t.Helper()
	root := projectRoot(t)
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "hayakv")
	build := exec.Command("go", "build", "-o", bin, "./cmd/hayakv")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hayakv: %v\n%s", err, out)
	}
	port := freeClusterPort(t)
	conf := filepath.Join(tmp, "redis.conf")
	if err := os.WriteFile(conf, []byte(fmt.Sprintf(`bind 127.0.0.1
port %d
dir %s
databases 16
net goroutine
engine shardmap
cluster-enable yes
cluster-mode redis
`, port, tmp)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "CONFIG="+conf)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start hayakv (cluster): %v", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	waitForPing(t, addr)
	return addr, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
}

// startRedis8Cluster spawns a dedicated cluster-enabled Redis 8. CLUSTER KEYSLOT
// requires cluster support to be enabled, and the external HAYAKV_DIFF_REDIS_ADDR
// instance is not assumed cluster-enabled, so a fresh node is always launched: a
// local redis-server binary is preferred, Docker is the fallback, and the test
// skips cleanly when neither is available.
func startRedis8Cluster(t *testing.T) (string, func()) {
	t.Helper()
	if path, err := exec.LookPath("redis-server"); err == nil {
		port := freeClusterPort(t)
		dir := t.TempDir()
		cmd := exec.Command(path,
			"--port", strconv.Itoa(port),
			"--cluster-enabled", "yes",
			"--cluster-config-file", filepath.Join(dir, "nodes.conf"),
			"--save", "",
			"--appendonly", "no",
			"--dir", dir,
		)
		if err := cmd.Start(); err != nil {
			t.Fatalf("start redis-server (cluster): %v", err)
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		waitForPing(t, addr)
		return addr, func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("no redis-server binary or docker; cannot run cluster diff")
	}
	infoCtx, infoCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer infoCancel()
	if err := exec.CommandContext(infoCtx, "docker", "info").Run(); err != nil {
		t.Skip("docker daemon not reachable; cannot run cluster diff")
	}
	port := freeClusterPort(t)
	name := fmt.Sprintf("hayakv-redis8-cluster-%d", port)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name,
		"-p", fmt.Sprintf("%d:6379", port), "redis:8", "redis-server",
		"--cluster-enabled", "yes", "--save", "", "--appendonly", "no")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start redis:8 (cluster): %v", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	waitForPing(t, addr)
	return addr, func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
		_ = cmd.Wait()
		cancel()
	}
}

// TestDifferentialCluster verifies that hayakv's CLUSTER KEYSLOT replies are
// byte-for-byte identical to real Redis 8. KEYSLOT is pure CRC16 + hash-tag
// extraction with no node-identity dependence, so it is the deterministic,
// diffable slice of the cluster command surface.
func TestDifferentialCluster(t *testing.T) {
	hayakvAddr, stop := startHayakvCluster(t)
	defer stop()
	redisAddr, stopR := startRedis8Cluster(t)
	defer stopR()
	for _, sc := range clusterCorpus() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			assertReplyEqual(t, sc, runScenario(t, hayakvAddr, sc), runScenario(t, redisAddr, sc))
		})
	}
}
