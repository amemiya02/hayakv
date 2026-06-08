package database

import (
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/persist/aof"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol/asserts"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestLoadRDB(t *testing.T) {
	tmpDir := t.TempDir()
	rdbPath := filepath.Join(tmpDir, "test.rdb")

	// Phase 1: populate a server and SAVE an RDB file (.gitignore excludes
	// *.rdb, so the fixture must be generated rather than checked in).
	config.Properties = &config.ServerProperties{
		Databases:      16,
		AppendOnly:     true,
		AppendFilename: filepath.Join(tmpDir, "gen.aof"),
		AppendFsync:    aof.FsyncAlways,
		RDBFilename:    rdbPath,
	}
	gen := NewStandaloneServer()
	defer gen.Close()
	conn := connection.NewFakeConn()
	asserts.AssertNotError(t, gen.Exec(conn, utils.ToCmdLine("Set", "str", "str")))
	asserts.AssertNotError(t, gen.Exec(conn, utils.ToCmdLine("RPush", "list", "1", "2", "3", "4")))
	asserts.AssertNotError(t, gen.Exec(conn, utils.ToCmdLine("HSet", "hash", "1", "1")))
	asserts.AssertNotError(t, gen.Exec(conn, utils.ToCmdLine("ZAdd", "zset", "1", "0", "1", "1")))
	asserts.AssertNotError(t, gen.Exec(conn, utils.ToCmdLine("SAdd", "set", "1")))
	asserts.AssertStatusReply(t, gen.Exec(conn, utils.ToCmdLine("Save")), "OK")

	// Phase 2: a fresh server with AOF disabled loads the RDB on boot.
	config.Properties = &config.ServerProperties{
		AppendOnly:  false,
		RDBFilename: rdbPath,
	}
	rdbDB := NewStandaloneServer()
	defer rdbDB.Close()
	result := rdbDB.Exec(conn, utils.ToCmdLine("Get", "str"))
	asserts.AssertBulkReply(t, result, "str")
	result = rdbDB.Exec(conn, utils.ToCmdLine("LRange", "list", "0", "-1"))
	asserts.AssertMultiBulkReply(t, result, []string{"1", "2", "3", "4"})
	result = rdbDB.Exec(conn, utils.ToCmdLine("HGetAll", "hash"))
	asserts.AssertMultiBulkReply(t, result, []string{"1", "1"})
	result = rdbDB.Exec(conn, utils.ToCmdLine("ZRange", "zset", "0", "-1", "WITHSCORES"))
	asserts.AssertMultiBulkReply(t, result, []string{"0", "1", "1", "1"})
	result = rdbDB.Exec(conn, utils.ToCmdLine("SCard", "set"))
	asserts.AssertIntReply(t, result, 1)

	// test no rdb file
	config.Properties = &config.ServerProperties{
		AppendOnly:  false,
		RDBFilename: "noexists.rdb",
	}
	rdbDB = NewStandaloneServer()
	defer rdbDB.Close()
	result = rdbDB.Exec(conn, utils.ToCmdLine("Get", "str"))
	asserts.AssertNullBulk(t, result)
}

func TestServerFsyncAlways(t *testing.T) {
	aofFile, err := ioutil.TempFile("", "*.aof")
	if err != nil {
		t.Error(err)
		return
	}
	config.Properties.AppendOnly = true
	config.Properties.AppendFilename = aofFile.Name()
	config.Properties.AppendFsync = aof.FsyncAlways
	server := NewStandaloneServer()
	defer server.Close()
	conn := connection.NewFakeConn()
	server.Exec(conn, utils.ToCmdLine("del", "1"))
	ret := server.Exec(conn, utils.ToCmdLine("incr", "1"))
	asserts.AssertNotError(t, ret)
	reader := NewStandaloneServer()
	defer reader.Close()
	ret = reader.Exec(conn, utils.ToCmdLine("get", "1"))
	asserts.AssertBulkReply(t, ret, "1")
}

func TestServerFsyncEverySec(t *testing.T) {
	aofFile, err := ioutil.TempFile("", "*.aof")
	if err != nil {
		t.Error(err)
		return
	}
	config.Properties.AppendOnly = true
	config.Properties.AppendFilename = aofFile.Name()
	config.Properties.AppendFsync = aof.FsyncEverySec
	server := NewStandaloneServer()
	defer server.Close()
	conn := connection.NewFakeConn()
	server.Exec(conn, utils.ToCmdLine("del", "1"))
	ret := server.Exec(conn, utils.ToCmdLine("incr", "1"))
	asserts.AssertNotError(t, ret)
	time.Sleep(1500 * time.Millisecond)
	reader := NewStandaloneServer()
	defer reader.Close()
	ret = reader.Exec(conn, utils.ToCmdLine("get", "1"))
	asserts.AssertBulkReply(t, ret, "1")
}
