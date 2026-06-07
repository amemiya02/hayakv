package diff

// NOTE: used_memory / INFO memory / approximate-LRU eviction order are non-deterministic;
// the expiry corpus covers only TTL/expire/PERSIST/EXPIRETIME and OOM-under-noeviction
// (byte-stable). Add normalization hooks before diffing memory.

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
engine redisdb
proto-max resp3
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

// startHayakvAuth starts hayakv with requirepass set.
func startHayakvAuth(t *testing.T, password string) (string, func()) {
	return startHayakvExtraConfig(t, "requirepass "+password+"\n")
}

// startRedis8Auth starts a Redis 8 instance with requirepass set.
// It configures authentication on whichever Redis backend is available
// (external via HAYAKV_DIFF_REDIS_ADDR or Docker).
func startRedis8Auth(t *testing.T, password string) (string, func()) {
	t.Helper()
	if addr := os.Getenv("HAYAKV_DIFF_REDIS_ADDR"); addr != "" {
		waitForPing(t, addr)
		// Set password on external Redis and reset on teardown
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial %s for auth setup: %v", addr, err)
		}
		reader := bufio.NewReader(conn)
		// Capture current password so we can restore it
		_, _ = conn.Write(encodeCommand([]string{"CONFIG", "GET", "requirepass"}))
		oldReply, _ := readReply(reader)
		_, _ = conn.Write(encodeCommand([]string{"CONFIG", "SET", "requirepass", password}))
		_, _ = readReply(reader)
		_ = conn.Close()
		return addr, func() {
			// Restore old password (best-effort)
			c, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err == nil {
				_, _ = c.Write(encodeCommand([]string{"AUTH", password}))
				r := bufio.NewReader(c)
				_, _ = readReply(r)
				_, _ = c.Write(append([]byte("*3\r\n$3\r\nCONFIG\r\n$3\r\nSET\r\n"), oldReply...))
				_, _ = readReply(r)
				_ = c.Close()
			}
		}
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
	name := fmt.Sprintf("hayakv-redis8-auth-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name,
		"-p", fmt.Sprintf("%d:6379", port),
		"redis:8", "redis-server", "--save", "", "--appendonly", "no",
		"--requirepass", password)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start redis:8 with auth: %v", err)
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

// ConnStep represents a single step on a specific connection in a multi-connection scenario.
type ConnStep struct {
	Conn int // connection index (0-based)
	Args []string
}

// MultiConnScenario describes a test that needs multiple simultaneous connections.
type MultiConnScenario struct {
	Name  string
	Conns int // number of connections to open
	Steps []ConnStep
}

// runScenarioMultiConn executes a scenario using multiple connections.
// It opens Conns connections, FLUSHALLs on conn 0, then executes each step
// on the specified connection, collecting replies.
func runScenarioMultiConn(t *testing.T, addr string, sc MultiConnScenario) [][]byte {
	t.Helper()
	conns := make([]net.Conn, sc.Conns)
	readers := make([]*bufio.Reader, sc.Conns)
	for i := range conns {
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		defer c.Close()
		conns[i], readers[i] = c, bufio.NewReader(c)
	}
	_, _ = conns[0].Write(encodeCommand([]string{"FLUSHALL"}))
	_, _ = readReply(readers[0])
	out := make([][]byte, 0, len(sc.Steps))
	for _, st := range sc.Steps {
		if _, err := conns[st.Conn].Write(encodeCommand(st.Args)); err != nil {
			t.Fatalf("write %v: %v", st.Args, err)
		}
		reply, err := readReply(readers[st.Conn])
		if err != nil {
			t.Fatalf("read %v: %v", st.Args, err)
		}
		out = append(out, reply)
	}
	return out
}

// assertReplyEqual compares hayakv and redis replies byte-for-byte,
// applying per-command Normalize hooks.
func assertReplyEqual(t *testing.T, sc Scenario, h, r [][]byte) {
	t.Helper()
	if len(h) != len(r) {
		t.Fatalf("%s reply count h=%d r=%d", sc.Name, len(h), len(r))
	}
	for i := range h {
		hi, ri := h[i], r[i]
		if fn := sc.Commands[i].Normalize; fn != nil {
			hi, ri = fn(hi), fn(ri)
		}
		if !bytes.Equal(hi, ri) {
			t.Fatalf("cmd %v\nhayakv: %q\nredis:  %q", sc.Commands[i].Args, hi, ri)
		}
	}
}

func TestDifferentialRESP3(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakvProto(t, "resp3")
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	hello := Command{Args: []string{"HELLO", "3"}}
	for _, scenario := range resp3Corpus() {
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

func TestDifferentialRedisDB(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakvWithEngine(t, "redisdb")
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range redisDBCorpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			hayakvReplies := runScenario(t, hayakvAddr, scenario)
			redisReplies := runScenario(t, redisAddr, scenario)
			if len(hayakvReplies) != len(redisReplies) {
				t.Fatalf("reply count hayakv=%d redis=%d", len(hayakvReplies), len(redisReplies))
			}
			for i := range hayakvReplies {
				hReply, rReply := hayakvReplies[i], redisReplies[i]
				if fn := scenario.Commands[i].Normalize; fn != nil {
					hReply = fn(hReply)
					rReply = fn(rReply)
				}
				if !bytes.Equal(hReply, rReply) {
					t.Fatalf("command %v\nhayakv: %q\nredis:  %q",
						scenario.Commands[i].Args, hReply, rReply)
				}
			}
		})
	}
}

func TestDifferentialRESP2(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakv(t)
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range baseCorpus() {
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

// startHayakvExtraConfig boots hayakv (goroutine+redisdb+resp2) with extra config lines
// appended (e.g. "maxmemory-policy allkeys-lru\n"). Mirrors startHayakvWithConfig
// but allows eviction/expiry keys the fixed helper cannot express.
func startHayakvExtraConfig(t *testing.T, extra string) (string, func()) {
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
	body := fmt.Sprintf("bind 127.0.0.1\nport %d\ndir %s\ndatabases 16\nnet goroutine\nengine redisdb\nproto-max resp2\n%s", port, tmp, extra)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
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

func TestDifferentialExpiry(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakvExtraConfig(t, "")
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range expiryCorpus() {
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
