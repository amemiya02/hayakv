package diff

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
	case '+', '-', ':':
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
	case '*':
		n, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return out, nil
		}
		for i := 0; i < n; i++ {
			child, err := readReply(r)
			if err != nil {
				return nil, err
			}
			out = append(out, child...)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported RESP prefix %q", prefix)
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

func waitForPing(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
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
		t.Skip("set HAYAKV_DIFF_REDIS_ADDR or install docker")
	}
	port := freePort(t)
	name := fmt.Sprintf("hayakv-redis8-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name, "-p", fmt.Sprintf("%d:6379", port), "redis:8", "redis-server", "--save", "", "--appendonly", "no")
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

func runScenario(t *testing.T, addr string, scenario Scenario) [][]byte {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	replies := make([][]byte, 0, len(scenario.Commands))
	for _, cmd := range scenario.Commands {
		if _, err := conn.Write(encodeCommand(cmd.Args)); err != nil {
			t.Fatalf("%s write %v: %v", addr, cmd.Args, err)
		}
		reply, err := readReply(reader)
		if err != nil {
			t.Fatalf("%s read %v: %v", addr, cmd.Args, err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestM0DifferentialRESP2(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakv(t)
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range m0Corpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			hayakvReplies := runScenario(t, hayakvAddr, scenario)
			redisReplies := runScenario(t, redisAddr, scenario)
			if len(hayakvReplies) != len(redisReplies) {
				t.Fatalf("reply count hayakv=%d redis=%d", len(hayakvReplies), len(redisReplies))
			}
			for i := range hayakvReplies {
				if !bytes.Equal(hayakvReplies[i], redisReplies[i]) {
					t.Fatalf("command %v\nhayakv: %q\nredis:  %q", scenario.Commands[i].Args, hayakvReplies[i], redisReplies[i])
				}
			}
		})
	}
}
