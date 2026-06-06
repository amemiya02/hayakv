package diff

// TODO: Add normalization hooks for non-deterministic commands (INFO, TIME, random)
// before adding them to the corpus. The current harness requires byte-for-byte equality.

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
	case '_': // RESP3 null
		return out, nil
	case '#', ',', '(': // RESP3 bool, double, bignum
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
	case '=': // RESP3 verbatim string
		n, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil {
			return nil, err
		}
		payload := make([]byte, n+2)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		return append(out, payload...), nil
	case '*', '%', '~', '>': // array, map, set, push
		n, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return out, nil
		}
		count := n
		if prefix == '%' { // map: n means n key-value pairs = 2n elements
			count = n * 2
		}
		for i := 0; i < count; i++ {
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
	return startHayakvProto(t, "resp2")
}

func startHayakvProto(t *testing.T, proto string) (string, func()) {
	return startHayakvWithConfig(t, "shardmap", proto)
}

func startHayakvWithEngine(t *testing.T, engine string) (string, func()) {
	return startHayakvWithConfig(t, engine, "resp2")
}

func startHayakvWithConfig(t *testing.T, engine, proto string) (string, func()) {
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
engine %s
proto-max %s
`, port, tmp, engine, proto)), 0o644); err != nil {
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

func startHayakvEventloop(t *testing.T) (string, func()) {
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
net eventloop
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
	// Check if Docker daemon is reachable
	infoCtx, infoCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer infoCancel()
	if err := exec.CommandContext(infoCtx, "docker", "info").Run(); err != nil {
		t.Skip("docker daemon not reachable; set HAYAKV_DIFF_REDIS_ADDR or start Docker")
	}
	port := freePort(t)
	name := fmt.Sprintf("hayakv-redis8-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	// Flush all keys before scenario for isolation
	if _, err := conn.Write(encodeCommand([]string{"FLUSHALL"})); err != nil {
		t.Fatalf("%s FLUSHALL write: %v", addr, err)
	}
	if _, err := readReply(reader); err != nil {
		t.Fatalf("%s FLUSHALL read: %v", addr, err)
	}
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

func TestM1DifferentialRESP3(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakvProto(t, "resp3")
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	hello := Command{Args: []string{"HELLO", "3"}}
	for _, scenario := range m1Corpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			withHello := Scenario{Name: scenario.Name,
				Commands: append([]Command{hello}, scenario.Commands...)}
			h := runScenario(t, hayakvAddr, withHello)[1:] // drop HELLO reply (server/version differ)
			r := runScenario(t, redisAddr, withHello)[1:]
			if len(h) != len(r) {
				t.Fatalf("reply count h=%d r=%d", len(h), len(r))
			}
			for i := range h {
				if !bytes.Equal(h[i], r[i]) {
					t.Fatalf("cmd %v\nhayakv: %q\nredis:  %q",
						scenario.Commands[i].Args, h[i], r[i])
				}
			}
		})
	}
}

func TestM2DifferentialRedisDB(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakvWithEngine(t, "redisdb")
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range m2Corpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			hayakvReplies := runScenario(t, hayakvAddr, scenario)
			redisReplies := runScenario(t, redisAddr, scenario)
			if len(hayakvReplies) != len(redisReplies) {
				t.Fatalf("reply count hayakv=%d redis=%d", len(hayakvReplies), len(redisReplies))
			}
			for i := range hayakvReplies {
				if !bytes.Equal(hayakvReplies[i], redisReplies[i]) {
					t.Fatalf("command %v\nhayakv: %q\nredis:  %q",
						scenario.Commands[i].Args, hayakvReplies[i], redisReplies[i])
				}
			}
		})
	}
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
