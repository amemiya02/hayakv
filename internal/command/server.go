package database

import (
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/persist/aof"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/pubsub"
	iscript "github.com/amemiya02/hayakv/internal/script"
)

var godisVersion = "1.2.8" // do not modify

// Server is a redis-server with full capabilities including multiple database, rdb loader, replication
type Server struct {
	dbSet []*atomic.Value // *DB

	// handle publish/subscribe
	hub *pubsub.Hub
	// handle aof persistence
	persister *aof.Persister

	// for replication
	role         int32
	slaveStatus  *slaveStatus
	masterStatus *masterStatus

	// hooks
	insertCallback database.KeyEventCallback
	deleteCallback database.KeyEventCallback

	// slow log record
	slogLogger *SlowLogger

	// activeExpireEnabled controls whether the active-expire cron runs.
	activeExpireEnabled bool

	// serverCronDone signals the serverCron goroutine to stop on Close().
	serverCronDone chan struct{}

	// replCronDone signals the replCron goroutine to stop on Close().
	replCronDone chan struct{}

	// disableCron prevents StartCron from launching (eventloop backend).
	disableCron bool

	// memMu serialises the entire denyoom path (pre-check → execute →
	// post-check/rollback) across all goroutines.  maxmemory is a global
	// constraint — concurrent denyoom writes on different keys must not
	// both pass the pre-check and then interfere in post-check.
	memMu sync.Mutex

	// scriptEngine provides EVAL/EVALSHA/SCRIPT support.
	scriptEngine iface.ScriptEngine

	// peakMemory tracks the high-water mark of usedMemory() for MEMORY STATS.
	peakMemory int64

	// cmdStats records per-command call count, latency, and error stats.
	cmdStats *cmdStats

	// latencyMon records latency events for LATENCY command.
	latencyMon *latencyMonitor
}

// DisableCron prevents StartCron from launching a background goroutine.
// Called when the eventloop net backend is active (single-threaded command execution).
func (server *Server) DisableCron() { server.disableCron = true }

// SetScriptEngine injects the scripting engine (called from backends.go to avoid import cycle).
func (server *Server) SetScriptEngine(e iface.ScriptEngine) {
	server.scriptEngine = e
}

// ExecFromScript is the invoker callback for redis.call/pcall inside Lua scripts.
// It runs commands through the normal Server.Exec path so writes propagate to AOF/replicas.
func (server *Server) ExecFromScript(c redis.Connection, cmdLine [][]byte) redis.Reply {
	return server.Exec(c, cmdLine)
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && !info.IsDir()
}

// NewStandaloneServer creates a standalone redis server, with multi database and all other funtions
func NewStandaloneServer() *Server {
	server := &Server{}
	if config.Properties.Databases == 0 {
		config.Properties.Databases = 16
	}
	// creat tmp dir
	err := os.MkdirAll(config.GetTmpDir(), os.ModePerm)
	if err != nil {
		panic(fmt.Errorf("create tmp dir failed: %v", err))
	}
	// make db set
	server.dbSet = make([]*atomic.Value, config.Properties.Databases)
	for i := range server.dbSet {
		singleDB := makeDB()
		singleDB.index = i
		singleDB.server = server
		holder := &atomic.Value{}
		holder.Store(singleDB)
		server.dbSet[i] = holder
	}
	server.hub = pubsub.MakeHub()
	// per-command stats (must be initialized before AOF replay calls Exec)
	server.cmdStats = newCmdStats()
	// latency monitor
	server.latencyMon = newLatencyMonitor()
	// record aof
	validAof := false
	if config.Properties.AppendOnly {
		validAof = fileExists(config.Properties.AppendFilename)
		aofHandler, err := NewPersister(server,
			config.Properties.AppendFilename, true, config.Properties.AppendFsync)
		if err != nil {
			panic(err)
		}
		server.bindPersister(aofHandler)
	}
	if config.Properties.RDBFilename != "" && !validAof {
		// load rdb
		err := server.loadRdbFile()
		if err != nil {
			logger.Error(err)
		}
	}
	server.activeExpireEnabled = true
	server.slaveStatus = initReplSlaveStatus()
	server.initMasterStatus()
	server.startReplCron()
	// StartCron is NOT started here. The goroutine backend calls
	// server.StartCron() explicitly after construction. The eventloop backend
	// never starts it (active-expire runs inline from the loop tick).
	server.role = masterRole // The initialization process does not require atomicity

	// record slow log
	server.slogLogger = NewSlowLogger(config.Properties.SlowLogMaxLen, config.Properties.SlowLogSlowerThan)

	// wire script engine
	busyMs := config.Properties.BusyReplyThreshold
	if busyMs <= 0 {
		busyMs = 5000
	}
	server.scriptEngine = iscript.NewEngine(server.ExecFromScript, busyMs)

	return server
}

// Exec executes command
// parameter `cmdLine` contains command and its arguments, for example: "set key value"
func (server *Server) Exec(c redis.Connection, cmdLine [][]byte) (result redis.Reply) {
	defer func() {
		if err := recover(); err != nil {
			logger.Warn(fmt.Sprintf("error occurs: %v\n%s", err, string(debug.Stack())))
			result = &protocol.UnknownErrReply{}
		}
	}()
	// Record the start time of command execution
	GodisExecCommandStartUnixTime := time.Now()

	cmdName := strings.ToLower(string(cmdLine[0]))
	// hello (must run before auth — clients send HELLO first)
	if cmdName == "hello" {
		return execHello(c, cmdLine[1:])
	}
	// ping
	if cmdName == "ping" {
		return Ping(c, cmdLine[1:])
	}
	// authenticate
	if cmdName == "auth" {
		return Auth(c, cmdLine[1:])
	}
	if !isAuthenticated(c) {
		return protocol.MakeErrReply("NOAUTH Authentication required")
	}
	// info
	if cmdName == "info" {
		return Info(server, cmdLine[1:])
	}
	if cmdName == "debug" {
		return execDebug(server, c, cmdLine[1:])
	}

	// scripting
	if cmdName == "eval" {
		return server.execEval(c, cmdLine[1:], false)
	}
	if cmdName == "eval_ro" {
		return server.execEval(c, cmdLine[1:], true)
	}
	if cmdName == "evalsha" {
		return server.execEvalSha(c, cmdLine[1:], false)
	}
	if cmdName == "evalsha_ro" {
		return server.execEvalSha(c, cmdLine[1:], true)
	}
	if cmdName == "script" {
		return server.execScript(c, cmdLine[1:])
	}

	// slowlog
	if cmdName == "slowlog" {
		return server.slogLogger.HandleSlowlogCommand(cmdLine)
	}

	// latency
	if cmdName == "latency" {
		return execLatency(server, cmdLine[1:])
	}

	if cmdName == "dbsize" {
		return DbSize(c, server)
	}
	if cmdName == "slaveof" || cmdName == "replicaof" {
		if c != nil && c.InMultiState() {
			return protocol.MakeErrReply("cannot use slave of database within multi")
		}
		if len(cmdLine) != 3 {
			return protocol.MakeArgNumErrReply(strings.ToUpper(cmdName))
		}
		return server.execSlaveOf(c, cmdLine[1:])
	} else if cmdName == "command" {
		return execCommand(cmdLine[1:])
	}
	if cmdName == "config" {
		return execConfig(server, cmdLine[1:])
	}
	if cmdName == "client" {
		return execClient(server, c, cmdLine[1:])
	}
	// monitor
	if cmdName == "monitor" {
		return execMonitor(server, c)
	}
	// reset
	if cmdName == "reset" {
		return execReset(server, c)
	}
	// latency
	if cmdName == "latency" {
		return execLatency(server, cmdLine[1:])
	}

	// read only slave
	role := atomic.LoadInt32(&server.role)
	if role == slaveRole && !c.IsMaster() {
		// only allow read only command, forbid all special commands except `auth` and `slaveof`
		if !isReadOnlyCommand(cmdName) {
			return protocol.MakeErrReply("READONLY You can't write against a read only slave.")
		}
	}

	// special commands which cannot execute within transaction
	if cmdName == "subscribe" {
		if len(cmdLine) < 2 {
			return protocol.MakeArgNumErrReply("subscribe")
		}
		return pubsub.Subscribe(server.hub, c, cmdLine[1:])
	} else if cmdName == "publish" {
		return pubsub.Publish(server.hub, cmdLine[1:])
	} else if cmdName == "unsubscribe" {
		return pubsub.UnSubscribe(server.hub, c, cmdLine[1:])
	} else if cmdName == "psubscribe" {
		if len(cmdLine) < 2 {
			return protocol.MakeArgNumErrReply("psubscribe")
		}
		return pubsub.PSubscribe(server.hub, c, cmdLine[1:])
	} else if cmdName == "punsubscribe" {
		return pubsub.PUnsubscribe(server.hub, c, cmdLine[1:])
	} else if cmdName == "pubsub" {
		return pubsub.PubSub(server.hub, cmdLine[1:])
	} else if cmdName == "bgrewriteaof" {
		if !config.Properties.AppendOnly {
			return protocol.MakeErrReply("AppendOnly is false, you can't rewrite aof file")
		}
		// aof.go imports router.go, router.go cannot import BGRewriteAOF from aof.go
		return BGRewriteAOF(server, cmdLine[1:])
	} else if cmdName == "rewriteaof" {
		if !config.Properties.AppendOnly {
			return protocol.MakeErrReply("AppendOnly is false, you can't rewrite aof file")
		}
		return RewriteAOF(server, cmdLine[1:])
	} else if cmdName == "flushall" {
		return server.flushAll()
	} else if cmdName == "flushdb" {
		if !validateArity(1, cmdLine) {
			return protocol.MakeArgNumErrReply(cmdName)
		}
		if c.InMultiState() {
			return protocol.MakeErrReply("ERR command 'FlushDB' cannot be used in MULTI")
		}
		return server.execFlushDB(c.GetDBIndex())
	} else if cmdName == "save" {
		return SaveRDB(server, cmdLine[1:])
	} else if cmdName == "bgsave" {
		return BGSaveRDB(server, cmdLine[1:])
	} else if cmdName == "select" {
		if c != nil && c.InMultiState() {
			return protocol.MakeErrReply("cannot select database within multi")
		}
		if len(cmdLine) != 2 {
			return protocol.MakeArgNumErrReply("select")
		}
		return execSelect(c, server, cmdLine[1:])
	} else if cmdName == "copy" {
		if len(cmdLine) < 3 {
			return protocol.MakeArgNumErrReply("copy")
		}
		return execCopy(server, c, cmdLine[1:])
	} else if cmdName == "replconf" {
		return server.execReplConf(c, cmdLine[1:])
	} else if cmdName == "psync" {
		return server.execPSync(c, cmdLine[1:])
	} else if cmdName == "wait" {
		return execWait(server, cmdLine[1:])
	}
	// todo: support multi database transaction

	// normal commands
	dbIndex := c.GetDBIndex()
	selectedDB, errReply := server.selectDB(dbIndex)
	if errReply != nil {
		return errReply
	}

	exec := selectedDB.Exec(c, cmdLine)
	// Record slow query logs
	server.slogLogger.Record(GodisExecCommandStartUnixTime, cmdLine, c.Name())
	// Record command stats
	usec := time.Since(GodisExecCommandStartUnixTime).Microseconds()
	isErr := exec != nil && len(exec.ToBytes()) > 0 && exec.ToBytes()[0] == '-'
	server.cmdStats.record(cmdName, usec, isErr)
	if isErr {
		server.cmdStats.recordError(errorPrefix(exec.ToBytes()))
	}
	// Record latency event if threshold is set
	if config.Properties.LatencyMonitorThreshold > 0 && server.latencyMon != nil {
		if usec/1000 >= int64(config.Properties.LatencyMonitorThreshold) {
			server.latencyMon.record("command", usec/1000)
		}
	}
	// Feed monitors
	if c != nil {
		feedMonitors(dbIndex, cmdLine, c.Name())
	}
	return exec
}

// AfterClientClose does some clean after client close connection
func (server *Server) AfterClientClose(c redis.Connection) {
	pubsub.UnsubscribeAll(server.hub, c)
	unregisterMonitor(c.ClientID())
}

// Close graceful shutdown database
func (server *Server) Close() {
	// stop repl cron
	if server.replCronDone != nil {
		close(server.replCronDone)
	}
	// stop server cron
	if server.serverCronDone != nil {
		close(server.serverCronDone)
	}
	// stop slaveStatus first
	server.slaveStatus.close()
	if server.persister != nil {
		server.persister.Close()
	}
	server.stopMaster()
}

func execSelect(c redis.Connection, mdb *Server, args [][]byte) redis.Reply {
	dbIndex, err := strconv.Atoi(string(args[0]))
	if err != nil {
		return protocol.MakeErrReply("ERR invalid DB index")
	}
	if dbIndex >= len(mdb.dbSet) || dbIndex < 0 {
		return protocol.MakeErrReply("ERR DB index is out of range")
	}
	c.SelectDB(dbIndex)
	return protocol.MakeOkReply()
}

func (server *Server) execFlushDB(dbIndex int) redis.Reply {
	if server.persister != nil {
		server.persister.SaveCmdLine(dbIndex, utils.ToCmdLine("FlushDB"))
	}
	return server.flushDB(dbIndex)
}

// flushDB flushes the selected database
func (server *Server) flushDB(dbIndex int) redis.Reply {
	if dbIndex >= len(server.dbSet) || dbIndex < 0 {
		return protocol.MakeErrReply("ERR DB index is out of range")
	}
	newDB := makeDB()
	server.loadDB(dbIndex, newDB)
	return &protocol.OkReply{}
}

func (server *Server) loadDB(dbIndex int, newDB *DB) redis.Reply {
	if dbIndex >= len(server.dbSet) || dbIndex < 0 {
		return protocol.MakeErrReply("ERR DB index is out of range")
	}
	oldDB := server.mustSelectDB(dbIndex)
	newDB.index = dbIndex
	newDB.server = server
	newDB.persister = oldDB.persister // inherit oldDB
	server.dbSet[dbIndex].Store(newDB)
	return &protocol.OkReply{}
}

// flushAll flushes all databases.
func (server *Server) flushAll() redis.Reply {
	for i := range server.dbSet {
		server.flushDB(i)
	}
	if server.persister != nil {
		server.persister.SaveCmdLine(0, utils.ToCmdLine("FlushAll"))
	}
	return &protocol.OkReply{}
}

// selectDB returns the database with the given index, or an error if the index is out of range.
func (server *Server) selectDB(dbIndex int) (*DB, *protocol.StandardErrReply) {
	if dbIndex >= len(server.dbSet) || dbIndex < 0 {
		return nil, protocol.MakeErrReply("ERR DB index is out of range")
	}
	return server.dbSet[dbIndex].Load().(*DB), nil
}

// mustSelectDB is like selectDB, but panics if an error occurs.
func (server *Server) mustSelectDB(dbIndex int) *DB {
	selectedDB, err := server.selectDB(dbIndex)
	if err != nil {
		panic(err)
	}
	return selectedDB
}

// ForEach traverses all the keys in the given database
func (server *Server) ForEach(dbIndex int, cb func(key string, data *database.DataEntity, expiration *time.Time) bool) {
	server.mustSelectDB(dbIndex).ForEach(cb)
}

// GetEntity returns the data entity to the given key
func (server *Server) GetEntity(dbIndex int, key string) (*database.DataEntity, bool) {
	return server.mustSelectDB(dbIndex).GetEntity(key)
}

func (server *Server) GetExpiration(dbIndex int, key string) *time.Time {
	raw, ok := server.mustSelectDB(dbIndex).ttlMap.Get(key)
	if !ok {
		return nil
	}
	expireTime, _ := raw.(time.Time)
	return &expireTime
}

// ExecMulti executes multi commands transaction Atomically and Isolated
func (server *Server) ExecMulti(conn redis.Connection, watching map[string]uint32, cmdLines []CmdLine) redis.Reply {
	selectedDB, errReply := server.selectDB(conn.GetDBIndex())
	if errReply != nil {
		return errReply
	}
	return selectedDB.ExecMulti(conn, watching, cmdLines)
}

// RWLocks lock keys for writing and reading
func (server *Server) RWLocks(dbIndex int, writeKeys []string, readKeys []string) {
	server.mustSelectDB(dbIndex).RWLocks(writeKeys, readKeys)
}

// RWUnLocks unlock keys for writing and reading
func (server *Server) RWUnLocks(dbIndex int, writeKeys []string, readKeys []string) {
	server.mustSelectDB(dbIndex).RWUnLocks(writeKeys, readKeys)
}

// GetUndoLogs return rollback commands
func (server *Server) GetUndoLogs(dbIndex int, cmdLine [][]byte) []CmdLine {
	return server.mustSelectDB(dbIndex).GetUndoLogs(cmdLine)
}

// ExecWithLock executes normal commands, invoker should provide locks
func (server *Server) ExecWithLock(conn redis.Connection, cmdLine [][]byte) redis.Reply {
	db, errReply := server.selectDB(conn.GetDBIndex())
	if errReply != nil {
		return errReply
	}
	return db.execWithLock(cmdLine)
}

// BGRewriteAOF asynchronously rewrites Append-Only-File
func BGRewriteAOF(db *Server, args [][]byte) redis.Reply {
	go db.persister.Rewrite()
	return protocol.MakeStatusReply("Background append only file rewriting started")
}

// RewriteAOF start Append-Only-File rewriting and blocked until it finished
func RewriteAOF(db *Server, args [][]byte) redis.Reply {
	err := db.persister.Rewrite()
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}
	return protocol.MakeOkReply()
}

// SaveRDB start RDB writing and blocked until it finished
func SaveRDB(db *Server, args [][]byte) redis.Reply {
	if db.persister == nil {
		return protocol.MakeErrReply("please enable aof before using save")
	}
	rdbFilename := config.Properties.RDBFilename
	if rdbFilename == "" {
		rdbFilename = "dump.rdb"
	}
	var err error
	if config.Properties.RdbImpl == "faithful" {
		err = db.saveFaithfulRDB(rdbFilename)
	} else {
		err = db.persister.GenerateRDB(rdbFilename)
	}
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}
	return protocol.MakeOkReply()
}

// BGSaveRDB asynchronously save RDB
func BGSaveRDB(db *Server, args [][]byte) redis.Reply {
	if db.persister == nil {
		return protocol.MakeErrReply("please enable aof before using save")
	}
	rdbFilename := config.Properties.RDBFilename
	if rdbFilename == "" {
		rdbFilename = "dump.rdb"
	}
	if config.Properties.RdbImpl == "faithful" {
		// Take the consistent point-in-time snapshot ON the caller's goroutine
		// (single-threaded => no key changes mid-copy), then encode in background.
		snap := db.snapshotAllDBs()
		dbCount := config.Properties.Databases
		go func() {
			defer func() {
				if err := recover(); err != nil {
					logger.Error(err)
				}
			}()
			if err := db.saveSnapshotToFile(snap, dbCount, rdbFilename); err != nil {
				logger.Error(err)
			}
		}()
		return protocol.MakeStatusReply("Background saving started")
	}
	// library fallback (unchanged)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logger.Error(err)
			}
		}()
		err := db.persister.GenerateRDB(rdbFilename)
		if err != nil {
			logger.Error(err)
		}
	}()
	return protocol.MakeStatusReply("Background saving started")
}

// GetDBSize returns keys count and ttl key count
func (server *Server) GetDBSize(dbIndex int) (int, int) {
	db := server.mustSelectDB(dbIndex)
	return db.data.Len(), db.ttlMap.Len()
}

func (server *Server) startReplCron() {
	server.replCronDone = make(chan struct{})
	go func(mdb *Server, done <-chan struct{}) {
		ticker := time.NewTicker(time.Second * 10)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mdb.slaveCron()
				mdb.masterCron()
			case <-done:
				return
			}
		}
	}(server, server.replCronDone)
}

// StartCron runs the hz-driven background cron: active expiration across
// all databases. Separate from the 10s startReplCron. Mirrors Redis serverCron's
// databasesCron -> activeExpireCycle.
func (server *Server) StartCron() {
	server.serverCronDone = make(chan struct{})
	go func(mdb *Server, done <-chan struct{}) {
		ticker := time.NewTicker(serverCronPeriod())
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mdb.activeExpireAllDBs()
			case <-done:
				return
			}
		}
	}(server, server.serverCronDone)
}

// activeExpireAllDBs runs one active-expire pass over every database.
func (server *Server) activeExpireAllDBs() {
	if !server.activeExpireEnabled {
		return
	}
	defer func() {
		if err := recover(); err != nil {
			logger.Warn(fmt.Sprintf("serverCron active-expire panic: %v", err))
		}
	}()
	for i := range server.dbSet {
		db := server.mustSelectDB(i)
		db.activeExpireCycle(activeExpireConfig{
			sampleSize: activeExpireKeysPerLoop,
			maxLoops:   16,
		})
	}
}

// GetAvgTTL Calculate the average expiration time of keys
func (server *Server) GetAvgTTL(dbIndex, randomKeyCount int) int64 {
	var ttlCount int64
	db := server.mustSelectDB(dbIndex)
	keys := db.data.RandomKeys(randomKeyCount)
	for _, k := range keys {
		t := time.Now()
		rawExpireTime, ok := db.ttlMap.Get(k)
		if !ok {
			continue
		}
		expireTime, _ := rawExpireTime.(time.Time)
		// if the key has already reached its expiration time during calculation, ignore it
		if expireTime.Sub(t).Microseconds() > 0 {
			ttlCount += expireTime.Sub(t).Microseconds()
		}
	}
	return ttlCount / int64(len(keys))
}

func (server *Server) SetKeyInsertedCallback(cb database.KeyEventCallback) {
	server.insertCallback = cb
	for i := range server.dbSet {
		db := server.mustSelectDB(i)
		db.insertCallback = cb
	}

}

// usedMemory returns the approximate total bytes held across all databases.
// Deterministic for fixed inputs; NOT byte-comparable with Redis used_memory.
func (server *Server) usedMemory() int64 {
	var total int64
	for i := range server.dbSet {
		db := server.mustSelectDB(i)
		db.data.ForEach(func(key string, raw interface{}) bool {
			entity, _ := raw.(*database.DataEntity)
			total += estimateEntitySize(key, entity)
			return true
		})
	}
	if total > server.peakMemory {
		server.peakMemory = total
	}
	return total
}

func (server *Server) SetKeyDeletedCallback(cb database.KeyEventCallback) {
	server.deleteCallback = cb
	for i := range server.dbSet {
		db := server.mustSelectDB(i)
		db.deleteCallback = cb
	}
}
