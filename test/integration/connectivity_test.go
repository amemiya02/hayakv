package integration

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForRedis(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_, _ = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
			_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			line, _, readErr := bufio.NewReader(conn).ReadLine()
			_ = conn.Close()
			if readErr == nil && string(line) == "+PONG" {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready", addr)
}

func projectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from test directory to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root (go.mod)")
		}
		dir = parent
	}
}

func startHayakv(t *testing.T) (addr string, stop func()) {
	return startHayakvProto(t, "resp2")
}

func startHayakvProto(t *testing.T, proto string) (addr string, stop func()) {
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
	if err := os.WriteFile(conf, []byte(fmt.Sprintf(`bind 127.0.0.1
port %d
dir %s
databases 16
net goroutine
engine shardmap
proto-max %s
`, port, tmp, proto)), 0o644); err != nil {
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

func TestRedisCLIConnectivity(t *testing.T) {
	if _, err := exec.LookPath("redis-cli"); err != nil {
		t.Skip("redis-cli not installed")
	}
	addr, stop := startHayakv(t)
	defer stop()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	out, err := exec.Command("redis-cli", "-h", host, "-p", port, "PING").CombinedOutput()
	if err != nil {
		t.Fatalf("redis-cli PING failed: %v\n%s", err, out)
	}
	if string(out) != "PONG\n" {
		t.Fatalf("redis-cli output = %q, want PONG", out)
	}
}

func TestGoRedisRESP2Connectivity(t *testing.T) {
	addr, stop := startHayakv(t)
	defer stop()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Protocol: 2,
	})
	defer client.Close()

	if got := client.Ping(ctx).Val(); got != "PONG" {
		t.Fatalf("PING = %q, want PONG", got)
	}
	if err := client.Set(ctx, "m0:key", "value", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if got := client.Get(ctx, "m0:key").Val(); got != "value" {
		t.Fatalf("GET = %q, want value", got)
	}
}
