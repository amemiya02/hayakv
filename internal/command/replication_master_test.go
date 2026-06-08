package database

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/persist/aof"
	"github.com/amemiya02/hayakv/internal/proto/resp2/parser"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
	"github.com/amemiya02/hayakv/internal/pubsub"
	"github.com/amemiya02/hayakv/internal/server/connection"
	rdb "github.com/hdt3213/rdb/parser"
)

func mockServer() *Server {
	server := &Server{}
	server.dbSet = make([]*atomic.Value, 16)
	for i := range server.dbSet {
		singleDB := makeDB()
		singleDB.index = i
		singleDB.server = server
		holder := &atomic.Value{}
		holder.Store(singleDB)
		server.dbSet[i] = holder
	}
	server.hub = pubsub.MakeHub()
	server.cmdStats = newCmdStats()
	server.latencyMon = newLatencyMonitor()
	server.slogLogger = NewSlowLogger(128, 10000)
	server.slaveStatus = initReplSlaveStatus()
	server.initMasterStatus()
	return server
}

func TestReplicationMasterSide(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "godis")
	if err != nil {
		t.Error(err)
		return
	}
	aofFilename := path.Join(tmpDir, "a.aof")
	defer func() {
		_ = os.Remove(aofFilename)
	}()
	config.Properties = &config.ServerProperties{
		Databases:      16,
		AppendOnly:     true,
		AppendFilename: aofFilename,
	}
	master := mockServer()
	aofHandler, err := NewPersister(master, config.Properties.AppendFilename, true, config.Properties.AppendFsync)
	if err != nil {
		panic(err)
	}
	master.bindPersister(aofHandler)
	slave := mockServer()
	replConn := connection.NewFakeConn()

	// set data to master
	masterConn := connection.NewFakeConn()
	resp := master.Exec(masterConn, utils.ToCmdLine("SET", "a", "a"))
	asserts.AssertNotError(t, resp)
	time.Sleep(time.Millisecond * 100) // wait write aof

	// full re-sync
	master.Exec(replConn, utils.ToCmdLine("psync", "?", "-1"))
	masterChan := parser.ParseStream(replConn)
	psyncPayload := <-masterChan
	if psyncPayload.Err != nil {
		t.Errorf("master bad protocol: %v", psyncPayload.Err)
		return
	}
	psyncHeader, ok := psyncPayload.Data.(*protocol.StatusReply)
	if !ok {
		t.Error("psync header is not a status reply")
		return
	}
	headers := strings.Split(psyncHeader.Status, " ")
	if len(headers) != 3 {
		t.Errorf("illegal psync header: %s", psyncHeader.Status)
		return
	}

	replId := headers[1]
	replOffset, err := strconv.ParseInt(headers[2], 10, 64)
	if err != nil {
		t.Errorf("illegal offset: %s", headers[2])
		return
	}
	t.Logf("repl id: %s, offset: %d", replId, replOffset)

	rdbPayload := <-masterChan
	if rdbPayload.Err != nil {
		t.Error("read response failed: " + rdbPayload.Err.Error())
		return
	}
	rdbReply, ok := rdbPayload.Data.(*protocol.BulkReply)
	if !ok {
		t.Error("illegal payload header: " + string(rdbPayload.Data.ToBytes()))
		return
	}

	rdbDec := rdb.NewDecoder(bytes.NewReader(rdbReply.Arg))
	err = slave.LoadRDB(rdbDec)
	if err != nil {
		t.Error("import rdb failed: " + err.Error())
		return
	}

	// get a
	slaveConn := connection.NewFakeConn()
	resp = slave.Exec(slaveConn, utils.ToCmdLine("get", "a"))
	asserts.AssertBulkReply(t, resp, "a")

	/*----  test broadcast aof  ----*/
	masterConn = connection.NewFakeConn()
	resp = master.Exec(masterConn, utils.ToCmdLine("SET", "b", "b"))
	time.Sleep(time.Millisecond * 100) // wait write aof
	asserts.AssertNotError(t, resp)
	master.masterCron()
	for {
		payload := <-masterChan
		if payload.Err != nil {
			t.Error(payload.Err)
			return
		}
		cmdLine, ok := payload.Data.(*protocol.MultiBulkReply)
		if !ok {
			t.Error("unexpected payload: " + string(payload.Data.ToBytes()))
			return
		}
		slave.Exec(replConn, cmdLine.Args)
		n := len(cmdLine.ToBytes())
		slave.slaveStatus.replOffset += int64(n)
		if string(cmdLine.Args[0]) != "ping" {
			break
		}
	}

	resp = slave.Exec(slaveConn, utils.ToCmdLine("get", "b"))
	asserts.AssertBulkReply(t, resp, "b")

	/*----  test partial reconnect  ----*/
	_ = replConn.Close() // mock disconnect

	replConn = connection.NewFakeConn()

	master.Exec(replConn, utils.ToCmdLine("psync", replId,
		strconv.FormatInt(slave.slaveStatus.replOffset, 10)))
	masterChan = parser.ParseStream(replConn)
	psyncPayload = <-masterChan
	if psyncPayload.Err != nil {
		t.Errorf("master bad protocol: %v", psyncPayload.Err)
		return
	}
	psyncHeader, ok = psyncPayload.Data.(*protocol.StatusReply)
	if !ok {
		t.Error("psync header is not a status reply")
		return
	}
	headers = strings.Split(psyncHeader.Status, " ")
	if len(headers) != 2 {
		t.Errorf("illegal psync header: %s", psyncHeader.Status)
		return
	}
	if headers[0] != "CONTINUE" {
		t.Errorf("expect CONTINUE actual %s", headers[0])
		return
	}
	replId = headers[1]
	t.Logf("partial resync repl id: %s, offset: %d", replId, slave.slaveStatus.replOffset)

	resp = master.Exec(masterConn, utils.ToCmdLine("SET", "c", "c"))
	time.Sleep(time.Millisecond * 100) // wait write aof
	asserts.AssertNotError(t, resp)
	master.masterCron()
	for {
		payload := <-masterChan
		if payload.Err != nil {
			t.Error(payload.Err)
			return
		}
		cmdLine, ok := payload.Data.(*protocol.MultiBulkReply)
		if !ok {
			t.Error("unexpected payload: " + string(payload.Data.ToBytes()))
			return
		}
		slave.Exec(replConn, cmdLine.Args)
		if string(cmdLine.Args[0]) != "ping" {
			break
		}
	}

	resp = slave.Exec(slaveConn, utils.ToCmdLine("get", "c"))
	asserts.AssertBulkReply(t, resp, "c")
}

func TestReplicationMasterRewriteRDB(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "godis")
	if err != nil {
		t.Error(err)
		return
	}
	aofFilename := path.Join(tmpDir, "a.aof")
	defer func() {
		_ = os.Remove(aofFilename)
	}()
	config.Properties = &config.ServerProperties{
		Databases:      16,
		AppendOnly:     true,
		AppendFilename: aofFilename,
		AppendFsync:    aof.FsyncAlways,
	}
	master := mockServer()
	aofHandler, err := NewPersister(master, config.Properties.AppendFilename, true, config.Properties.AppendFsync)
	if err != nil {
		panic(err)
	}
	master.bindPersister(aofHandler)

	masterConn := connection.NewFakeConn()
	resp := master.Exec(masterConn, utils.ToCmdLine("SET", "a", "a"))
	asserts.AssertNotError(t, resp)
	resp = master.Exec(masterConn, utils.ToCmdLine("SET", "b", "b"))
	asserts.AssertNotError(t, resp)
	time.Sleep(time.Millisecond * 100) // wait write aof

	err = master.rewriteRDB()
	if err != nil {
		t.Error(err)
		return
	}
	resp = master.Exec(masterConn, utils.ToCmdLine("SET", "c", "c"))
	asserts.AssertNotError(t, resp)
	time.Sleep(time.Millisecond * 100) // wait write aof

	// set slave
	slave := mockServer()
	replConn := connection.NewFakeConn()
	master.Exec(replConn, utils.ToCmdLine("psync", "?", "-1"))
	masterChan := parser.ParseStream(replConn)
	psyncPayload := <-masterChan
	if psyncPayload.Err != nil {
		t.Errorf("master bad protocol: %v", psyncPayload.Err)
		return
	}
	psyncHeader, ok := psyncPayload.Data.(*protocol.StatusReply)
	if !ok {
		t.Error("psync header is not a status reply")
		return
	}
	headers := strings.Split(psyncHeader.Status, " ")
	if len(headers) != 3 {
		t.Errorf("illegal psync header: %s", psyncHeader.Status)
		return
	}

	replId := headers[1]
	replOffset, err := strconv.ParseInt(headers[2], 10, 64)
	if err != nil {
		t.Errorf("illegal offset: %s", headers[2])
		return
	}
	t.Logf("repl id: %s, offset: %d", replId, replOffset)

	rdbPayload := <-masterChan
	if rdbPayload.Err != nil {
		t.Error("read response failed: " + rdbPayload.Err.Error())
		return
	}
	rdbReply, ok := rdbPayload.Data.(*protocol.BulkReply)
	if !ok {
		t.Error("illegal payload header: " + string(rdbPayload.Data.ToBytes()))
		return
	}

	rdbDec := rdb.NewDecoder(bytes.NewReader(rdbReply.Arg))
	err = slave.LoadRDB(rdbDec)
	if err != nil {
		t.Error("import rdb failed: " + err.Error())
		return
	}

	slaveConn := connection.NewFakeConn()
	resp = slave.Exec(slaveConn, utils.ToCmdLine("get", "a"))
	asserts.AssertBulkReply(t, resp, "a")
	resp = slave.Exec(slaveConn, utils.ToCmdLine("get", "b"))
	asserts.AssertBulkReply(t, resp, "b")

	master.masterCron()
	for {
		payload := <-masterChan
		if payload.Err != nil {
			t.Error(payload.Err)
			return
		}
		cmdLine, ok := payload.Data.(*protocol.MultiBulkReply)
		if !ok {
			t.Error("unexpected payload: " + string(payload.Data.ToBytes()))
			return
		}
		slave.Exec(replConn, cmdLine.Args)
		n := len(cmdLine.ToBytes())
		slave.slaveStatus.replOffset += int64(n)
		if string(cmdLine.Args[0]) != "ping" {
			break
		}
	}
	resp = slave.Exec(slaveConn, utils.ToCmdLine("get", "c"))
	asserts.AssertBulkReply(t, resp, "c")
}

func TestBacklogTrimAdvancesBeginOffset(t *testing.T) {
	b := &replBacklog{}
	b.setLimit(8)                 // keep at most 8 bytes
	b.appendBytes([]byte("abcd")) // begin=0 cur=4 buf="abcd"
	b.appendBytes([]byte("efgh")) // begin=0 cur=8 buf="abcdefgh"
	if b.beginOffset != 0 || b.currentOffset != 8 || string(b.buf) != "abcdefgh" {
		t.Fatalf("pre-trim: begin=%d cur=%d buf=%q", b.beginOffset, b.currentOffset, b.buf)
	}
	b.appendBytes([]byte("ij")) // would be 10 bytes; trim to last 8 → drop "ab"
	if b.beginOffset != 2 {
		t.Fatalf("beginOffset = %d, want 2", b.beginOffset)
	}
	if b.currentOffset != 10 {
		t.Fatalf("currentOffset = %d, want 10", b.currentOffset)
	}
	if string(b.buf) != "cdefghij" {
		t.Fatalf("buf = %q, want %q", b.buf, "cdefghij")
	}
}

func TestBacklogValidOffsetAfterTrim(t *testing.T) {
	b := &replBacklog{}
	b.setLimit(4)
	b.appendBytes([]byte("abcdef")) // trims to "cdef", begin=2 cur=6
	if b.isValidOffset(1) {         // 1 < beginOffset(2): dropped, must be invalid
		t.Fatalf("offset 1 should be invalid after trim")
	}
	if !b.isValidOffset(2) { // == beginOffset: still in window
		t.Fatalf("offset 2 should be valid")
	}
	if !b.isValidOffset(5) { // within [2,6]
		t.Fatalf("offset 5 should be valid")
	}
	if !b.isValidOffset(6) { // == currentOffset: caught-up replica, valid +CONTINUE
		t.Fatalf("offset 6 should be valid (caught-up replica)")
	}
	snap, cur := b.getSnapshotAfter(4) // bytes from offset 4 → "ef"
	if string(snap) != "ef" || cur != 6 {
		t.Fatalf("getSnapshotAfter(4) = %q,%d want \"ef\",6", snap, cur)
	}
}

func TestBacklogZeroLimitDoesNotTrim(t *testing.T) {
	b := &replBacklog{} // limit 0 = unbounded (back-compat)
	b.appendBytes([]byte("abcdefghij"))
	if b.beginOffset != 0 || string(b.buf) != "abcdefghij" {
		t.Fatalf("zero limit must not trim: begin=%d buf=%q", b.beginOffset, b.buf)
	}
}
