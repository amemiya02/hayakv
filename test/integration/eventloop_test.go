package integration

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func startHayakvEventloop(t *testing.T) (addr string, stop func()) {
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
	waitForRedis(t, addr)

	return addr, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
}

func encodeCmd(args ...string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	return b.Bytes()
}

func readRESP(r *bufio.Reader) (string, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		return "", err
	}
	out := string(append([]byte{prefix}, line...))

	switch prefix {
	case '+', '-', ':':
		return out, nil
	case '$':
		n, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil {
			return "", err
		}
		if n < 0 {
			return out, nil
		}
		payload := make([]byte, n+2)
		if _, err := r.Read(payload); err != nil {
			return "", err
		}
		return out + string(payload), nil
	case '*':
		n, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil {
			return "", err
		}
		if n < 0 {
			return out, nil
		}
		result := out
		for i := 0; i < n; i++ {
			child, err := readRESP(r)
			if err != nil {
				return "", err
			}
			result += child
		}
		return result, nil
	default:
		return out, nil
	}
}

func TestEventloopConnectivity(t *testing.T) {
	addr, stop := startHayakvEventloop(t)
	defer stop()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// PING
	if _, err := conn.Write(encodeCmd("PING")); err != nil {
		t.Fatalf("write PING: %v", err)
	}
	reader := bufio.NewReader(conn)
	reply, err := readRESP(reader)
	if err != nil {
		t.Fatalf("read PING: %v", err)
	}
	if !strings.Contains(reply, "PONG") {
		t.Fatalf("PING reply = %q, want PONG", reply)
	}
}

func TestEventloopSetGet(t *testing.T) {
	addr, stop := startHayakvEventloop(t)
	defer stop()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// SET
	if _, err := conn.Write(encodeCmd("SET", "mykey", "myval")); err != nil {
		t.Fatalf("write SET: %v", err)
	}
	reply, err := readRESP(reader)
	if err != nil {
		t.Fatalf("read SET: %v", err)
	}
	if !strings.Contains(reply, "OK") {
		t.Fatalf("SET reply = %q, want OK", reply)
	}

	// GET
	if _, err := conn.Write(encodeCmd("GET", "mykey")); err != nil {
		t.Fatalf("write GET: %v", err)
	}
	reply, err = readRESP(reader)
	if err != nil {
		t.Fatalf("read GET: %v", err)
	}
	if !strings.Contains(reply, "myval") {
		t.Fatalf("GET reply = %q, want myval", reply)
	}
}

func TestEventloopMultiplePipelined(t *testing.T) {
	addr, stop := startHayakvEventloop(t)
	defer stop()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// Send SET + GET in one write (pipelining).
	data := append(encodeCmd("SET", "pk", "pv"), encodeCmd("GET", "pk")...)
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}

	// Read SET reply.
	r1, err := readRESP(reader)
	if err != nil {
		t.Fatalf("read SET: %v", err)
	}
	if !strings.Contains(r1, "OK") {
		t.Fatalf("SET reply = %q, want OK", r1)
	}

	// Read GET reply.
	r2, err := readRESP(reader)
	if err != nil {
		t.Fatalf("read GET: %v", err)
	}
	if !strings.Contains(r2, "pv") {
		t.Fatalf("GET reply = %q, want pv", r2)
	}
}

func TestEventloopFlushAll(t *testing.T) {
	addr, stop := startHayakvEventloop(t)
	defer stop()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	if _, err := conn.Write(encodeCmd("FLUSHALL")); err != nil {
		t.Fatalf("write FLUSHALL: %v", err)
	}
	reply, err := readRESP(reader)
	if err != nil {
		t.Fatalf("read FLUSHALL: %v", err)
	}
	if !strings.Contains(reply, "OK") {
		t.Fatalf("FLUSHALL reply = %q, want OK", reply)
	}
}

// TestEventloopXReadBlockUnblocks verifies that XREAD BLOCK unblocks when
// XADD adds a new entry to the stream.
func TestEventloopXReadBlockUnblocks(t *testing.T) {
	addr, stop := startHayakvEventloop(t)
	defer stop()

	// Reader connection: issues XREAD BLOCK.
	readerConn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial reader: %v", err)
	}
	defer readerConn.Close()
	readerR := bufio.NewReader(readerConn)

	// Writer connection: issues XADD.
	writerConn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial writer: %v", err)
	}
	defer writerConn.Close()
	writerR := bufio.NewReader(writerConn)

	// FLUSHALL first.
	if _, err := readerConn.Write(encodeCmd("FLUSHALL")); err != nil {
		t.Fatalf("write FLUSHALL: %v", err)
	}
	if _, err := readRESP(readerR); err != nil {
		t.Fatalf("read FLUSHALL: %v", err)
	}

	// Start XREAD BLOCK 5000 STREAMS mystream $ in a goroutine.
	type readResult struct {
		reply string
		err   error
	}
	ch := make(chan readResult, 1)
	go func() {
		// XREAD BLOCK 5000 STREAMS mystream $
		xreadCmd := encodeCmd("XREAD", "BLOCK", "5000", "STREAMS", "mystream", "$")
		if _, err := readerConn.Write(xreadCmd); err != nil {
			ch <- readResult{"", err}
			return
		}
		reply, err := readRESP(readerR)
		ch <- readResult{reply, err}
	}()

	// Give the reader time to block.
	time.Sleep(300 * time.Millisecond)

	// XADD a value to mystream.
	xaddCmd := encodeCmd("XADD", "mystream", "*", "f1", "v1")
	if _, err := writerConn.Write(xaddCmd); err != nil {
		t.Fatalf("write XADD: %v", err)
	}
	// Read XADD reply (the assigned ID).
	xaddReply, err := readRESP(writerR)
	if err != nil {
		t.Fatalf("read XADD: %v", err)
	}
	if !strings.Contains(xaddReply, "-") { // ID contains '-'
		t.Fatalf("XADD reply = %q, want stream ID", xaddReply)
	}

	// Wait for the reader to unblock.
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("XREAD BLOCK error: %v", res.err)
		}
		// The reply should contain "mystream" and "f1" and "v1".
		if !strings.Contains(res.reply, "mystream") {
			t.Fatalf("XREAD BLOCK reply = %q, want stream name 'mystream'", res.reply)
		}
		if !strings.Contains(res.reply, "f1") {
			t.Fatalf("XREAD BLOCK reply = %q, want field 'f1'", res.reply)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("XREAD BLOCK did not unblock within timeout")
	}
}

// TestEventloopXReadBlockTimeout verifies that XREAD BLOCK returns null
// when the timeout expires without any new entries.
func TestEventloopXReadBlockTimeout(t *testing.T) {
	addr, stop := startHayakvEventloop(t)
	defer stop()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// FLUSHALL first.
	if _, err := conn.Write(encodeCmd("FLUSHALL")); err != nil {
		t.Fatalf("write FLUSHALL: %v", err)
	}
	if _, err := readRESP(reader); err != nil {
		t.Fatalf("read FLUSHALL: %v", err)
	}

	start := time.Now()
	// XREAD BLOCK 500 STREAMS mystream $
	xreadCmd := encodeCmd("XREAD", "BLOCK", "500", "STREAMS", "mystream", "$")
	if _, err := conn.Write(xreadCmd); err != nil {
		t.Fatalf("write XREAD: %v", err)
	}
	reply, err := readRESP(reader)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("read XREAD: %v", err)
	}
	// Should return null (*-1\r\n) after ~500ms.
	if !strings.Contains(reply, "*-1") {
		t.Fatalf("XREAD BLOCK timeout reply = %q, want *-1 (null)", reply)
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("XREAD BLOCK returned too quickly: %v", elapsed)
	}
}
