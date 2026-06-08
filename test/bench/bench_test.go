package bench

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBenchVsRedis(t *testing.T) {
	if os.Getenv("HAYAKV_BENCH") == "" {
		t.Skip("set HAYAKV_BENCH=1 to run benchmark comparison")
	}

	if _, err := exec.LookPath("redis-benchmark"); err != nil {
		t.Skip("redis-benchmark not found; install redis-tools")
	}

	hayakvAddr, stopHayakv := startHayakv(t)
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	t.Log("running hayakv benchmarks...")
	hayakv, err := RunSuite(hayakvAddr)
	if err != nil {
		t.Fatalf("hayakv RunSuite: %v", err)
	}

	t.Log("running redis benchmarks...")
	redis, err := RunSuite(redisAddr)
	if err != nil {
		t.Fatalf("redis RunSuite: %v", err)
	}

	// Record ratios — never assert a threshold.
	t.Log("--- benchmark ratios (hayakv / redis) ---")
	for key, hOps := range hayakv {
		rOps := redis[key]
		ratio := 0.0
		if rOps > 0 {
			ratio = hOps / rOps
		}
		t.Logf("%-20s  hayakv=%12.1f  redis=%12.1f  ratio=%.3f", key, hOps, rOps, ratio)
	}

	// Write results to a file so CI can fold them into the scoreboard.
	writeResults(t, hayakv, redis)
}

func writeResults(t *testing.T, hayakv, redis map[string]float64) {
	t.Helper()
	dir := filepath.Join(projectRoot(t), "test", "bench")
	path := filepath.Join(dir, "results.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Logf("warning: cannot write results: %v", err)
		return
	}
	defer f.Close()
	fmt.Fprintln(f, "{")
	first := true
	for key, hOps := range hayakv {
		if !first {
			fmt.Fprintln(f, ",")
		}
		first = false
		rOps := redis[key]
		fmt.Fprintf(f, "  %q: {hayakv: %.1f, redis: %.1f}", key, hOps, rOps)
	}
	fmt.Fprintln(f, "\n}")
	t.Logf("results written to %s", path)
}

// --- helpers (mirrored from test/diff for server bootstrap) ---

func projectRoot(t *testing.T) string {
	t.Helper()
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

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func encodeCommand(args []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	return b.Bytes()
}

func readReply(r *bufio.Reader) ([]byte, error) {
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
		n, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return out, nil
		}
		payload := make([]byte, n+2)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		return append(out, payload...), nil
	default:
		return nil, fmt.Errorf("unsupported RESP prefix %q", prefix)
	}
}

func waitForPing(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_, _ = conn.Write(encodeCommand([]string{"PING"}))
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			got, readErr := readReply(bufio.NewReader(conn))
			_ = conn.Close()
			if readErr == nil && bytes.Equal(got, []byte("+PONG\r\n")) {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s did not answer PING", addr)
}

func startHayakv(t *testing.T) (string, func()) {
	t.Helper()
	root := projectRoot(t)
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "hayakv")
	build := exec.Command("go", "build", "-o", bin, "./cmd/hayakv")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hayakv: %v\n%s", err, out)
	}
	port := freePort(t)
	conf := filepath.Join(tmp, "redis.conf")
	if err := os.WriteFile(conf, []byte(fmt.Sprintf(`bind 127.0.0.1
port %d
dir %s
databases 16
net goroutine
engine shardmap
proto-max resp2
`, port, tmp)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "CONFIG="+conf)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start hayakv: %v", err)
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

func startRedis8(t *testing.T) (string, func()) {
	t.Helper()
	if addr := os.Getenv("HAYAKV_DIFF_REDIS_ADDR"); addr != "" {
		waitForPing(t, addr)
		return addr, func() {}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed; set HAYAKV_DIFF_REDIS_ADDR to test against an external Redis 8")
	}
	infoCtx, infoCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer infoCancel()
	if err := exec.CommandContext(infoCtx, "docker", "info").Run(); err != nil {
		t.Skip("docker daemon not reachable; set HAYAKV_DIFF_REDIS_ADDR or start Docker")
	}
	port := freePort(t)
	name := fmt.Sprintf("hayakv-bench-redis8-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name,
		"-p", fmt.Sprintf("%d:6379", port), "redis:8",
		"redis-server", "--save", "", "--appendonly", "no")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start redis:8: %v", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	waitForPing(t, addr)
	return addr, func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
		_ = cmd.Wait()
		cancel()
	}
}
