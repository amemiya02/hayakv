package diff

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// startHayakvFaithful runs hayakv with the faithful RDB codec and appendonly on,
// in tmpDir, returning addr + the tmp dir (so we can read the dump.rdb it writes).
func startHayakvFaithful(t *testing.T, tmp string) (string, func()) {
	t.Helper()
	root := projectRoot(t)
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
engine redisdb
proto-max resp2
appendonly yes
appendfilename appendonly.aof
appendfsync everysec
dbfilename dump.rdb
rdb-impl faithful
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

// sendAndRead writes one command and reads one reply.
func sendAndRead(t *testing.T, addr string, args ...string) []byte {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	if _, err := conn.Write(encodeCommand(args)); err != nil {
		t.Fatalf("write %v: %v", args, err)
	}
	reply, err := readReply(r)
	if err != nil {
		t.Fatalf("read %v: %v", args, err)
	}
	return reply
}

func TestRDBCrossLoadHayakvToRedis(t *testing.T) {
	if _, err := exec.LookPath("redis-server"); err != nil {
		t.Skip("redis-server not on PATH; skipping hayakv->redis cross-load")
	}
	tmp := t.TempDir()
	hAddr, stopH := startHayakvFaithful(t, tmp)

	// populate hayakv across types
	sendAndRead(t, hAddr, "SET", "str", "hello")
	sendAndRead(t, hAddr, "RPUSH", "list", "a", "b", "c")
	sendAndRead(t, hAddr, "SADD", "set", "x", "y")
	sendAndRead(t, hAddr, "HSET", "hash", "f1", "v1", "f2", "v2")
	sendAndRead(t, hAddr, "ZADD", "zset", "1", "m1", "2", "m2")
	if got := sendAndRead(t, hAddr, "SAVE"); !bytes.Equal(got, []byte("+OK\r\n")) {
		t.Fatalf("SAVE = %q", got)
	}
	stopH() // hayakv must release the file before redis loads it

	// boot redis pointed at hayakv's dump.rdb (compression disabled to avoid LZF)
	port := freePort(t)
	rcmd := exec.Command("redis-server",
		"--port", fmt.Sprintf("%d", port),
		"--dir", tmp,
		"--dbfilename", "dump.rdb",
		"--rdbcompression", "no",
		"--appendonly", "no",
		"--save", "")
	if err := rcmd.Start(); err != nil {
		t.Fatalf("start redis: %v", err)
	}
	rAddr := fmt.Sprintf("127.0.0.1:%d", port)
	defer func() {
		if rcmd.Process != nil {
			_ = rcmd.Process.Kill()
			_, _ = rcmd.Process.Wait()
		}
	}()
	waitForPing(t, rAddr)

	// real redis must serve every key hayakv persisted
	if got := sendAndRead(t, rAddr, "GET", "str"); !bytes.Equal(got, []byte("$5\r\nhello\r\n")) {
		t.Fatalf("redis GET str = %q", got)
	}
	if got := sendAndRead(t, rAddr, "LRANGE", "list", "0", "-1"); !bytes.Equal(got, []byte("*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n")) {
		t.Fatalf("redis LRANGE list = %q", got)
	}
	if got := sendAndRead(t, rAddr, "HGET", "hash", "f1"); !bytes.Equal(got, []byte("$2\r\nv1\r\n")) {
		t.Fatalf("redis HGET hash f1 = %q", got)
	}
	if got := sendAndRead(t, rAddr, "ZSCORE", "zset", "m2"); !bytes.Equal(got, []byte("$1\r\n2\r\n")) {
		t.Fatalf("redis ZSCORE zset m2 = %q", got)
	}
	if got := sendAndRead(t, rAddr, "SCARD", "set"); !bytes.Equal(got, []byte(":2\r\n")) {
		t.Fatalf("redis SCARD set = %q", got)
	}
}

func TestRDBCrossLoadRedisToHayakv(t *testing.T) {
	if _, err := exec.LookPath("redis-server"); err != nil {
		t.Skip("redis-server not on PATH; skipping redis->hayakv cross-load")
	}
	tmp := t.TempDir()

	// 1) redis writes dump.rdb (no compression -> no LZF in the output)
	rport := freePort(t)
	rcmd := exec.Command("redis-server",
		"--port", fmt.Sprintf("%d", rport),
		"--dir", tmp,
		"--dbfilename", "dump.rdb",
		"--rdbcompression", "no",
		"--appendonly", "no",
		"--save", "")
	if err := rcmd.Start(); err != nil {
		t.Fatalf("start redis: %v", err)
	}
	rAddr := fmt.Sprintf("127.0.0.1:%d", rport)
	waitForPing(t, rAddr)
	sendAndRead(t, rAddr, "SET", "str", "hello")
	sendAndRead(t, rAddr, "RPUSH", "list", "a", "b", "c")
	sendAndRead(t, rAddr, "SADD", "set", "x", "y")
	sendAndRead(t, rAddr, "HSET", "hash", "f1", "v1")
	sendAndRead(t, rAddr, "ZADD", "zset", "2", "m2")
	if got := sendAndRead(t, rAddr, "SAVE"); !bytes.Equal(got, []byte("+OK\r\n")) {
		t.Fatalf("redis SAVE = %q", got)
	}
	if rcmd.Process != nil {
		_ = rcmd.Process.Kill()
		_, _ = rcmd.Process.Wait()
	}

	// 2) hayakv (faithful, appendonly OFF so it loads the rdb) boots on that dir
	root := projectRoot(t)
	bin := filepath.Join(tmp, "hayakv")
	build := exec.Command("go", "build", "-o", bin, "./cmd/hayakv")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hayakv: %v\n%s", err, out)
	}
	hport := freePort(t)
	conf := filepath.Join(tmp, "redis.conf")
	if err := os.WriteFile(conf, []byte(fmt.Sprintf(`bind 127.0.0.1
port %d
dir %s
databases 16
net goroutine
engine redisdb
proto-max resp2
appendonly no
dbfilename dump.rdb
rdb-impl faithful
`, hport, tmp)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	hcmd := exec.Command(bin)
	hcmd.Env = append(os.Environ(), "CONFIG="+conf)
	if err := hcmd.Start(); err != nil {
		t.Fatalf("start hayakv: %v", err)
	}
	hAddr := fmt.Sprintf("127.0.0.1:%d", hport)
	defer func() {
		if hcmd.Process != nil {
			_ = hcmd.Process.Kill()
			_, _ = hcmd.Process.Wait()
		}
	}()
	waitForPing(t, hAddr)

	// 3) hayakv must serve every key redis persisted
	if got := sendAndRead(t, hAddr, "GET", "str"); !bytes.Equal(got, []byte("$5\r\nhello\r\n")) {
		t.Fatalf("hayakv GET str = %q", got)
	}
	if got := sendAndRead(t, hAddr, "LRANGE", "list", "0", "-1"); !bytes.Equal(got, []byte("*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n")) {
		t.Fatalf("hayakv LRANGE list = %q", got)
	}
	if got := sendAndRead(t, hAddr, "SCARD", "set"); !bytes.Equal(got, []byte(":2\r\n")) {
		t.Fatalf("hayakv SCARD set = %q", got)
	}
	if got := sendAndRead(t, hAddr, "HGET", "hash", "f1"); !bytes.Equal(got, []byte("$2\r\nv1\r\n")) {
		t.Fatalf("hayakv HGET hash f1 = %q", got)
	}
	if got := sendAndRead(t, hAddr, "ZSCORE", "zset", "m2"); !bytes.Equal(got, []byte("$1\r\n2\r\n")) {
		t.Fatalf("hayakv ZSCORE zset m2 = %q", got)
	}
}
