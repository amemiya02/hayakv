// Package database is a memory database with redis compatible interface
package database

import (
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/timewheel"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/proto/resp3"
)

const (
	dataDictSize = 1 << 16
	ttlDictSize  = 1 << 10
)

// aofBuffers is per-command storage for deferred AOF buffering, keyed by
// goroutine ID.  A DenyOOM command stores its buffer here before the
// executor runs; addAof checks it on every call so the executor's AOF
// writes are captured without swapping any shared function-pointer field
// (which would race with concurrent commands on other keys / MULTI /
// active-expire).  Safe for concurrent access: each goroutine only writes
// its own entry; Load is lock-free in the common (non-buffering) path.
var aofBuffers sync.Map // int (goid) → *[]CmdLine

// goid returns the current goroutine's ID by parsing the runtime stack.
// ~150 ns; only called from the AOF write path (every command) so the
// cost is negligible compared to the I/O it gates.
func goid() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// "goroutine 123 [..."
	var id int
	for i := 10; i < n; i++ {
		if buf[i] < '0' || buf[i] > '9' {
			break
		}
		id = id*10 + int(buf[i]-'0')
	}
	return id
}

// DB stores data and execute user's commands
type DB struct {
	index int
	// key -> DataEntity
	data dict.Dict
	// key -> expireTime (time.Time)
	ttlMap *dict.ConcurrentDict
	// key -> version(uint32)
	versionMap *dict.ConcurrentDict
	// key -> lruMeta (approximate LRU clock OR LFU counter, per maxmemory-policy)
	lruMap *dict.ConcurrentDict

	// persister is the real AOF handler (set by bindPersister).
	// Never swapped at runtime — the addAof method wraps it with
	// per-command buffering when needed.
	persister func(CmdLine)

	// callbacks
	insertCallback database.KeyEventCallback
	deleteCallback database.KeyEventCallback

	// owning server (for cross-db memory accounting / eviction)
	server *Server
}

// addAof is the single entry-point for all AOF writes.
// If a DenyOOM command is buffering on this goroutine, the write goes into
// that buffer; otherwise it goes directly to the persister.
// Because it is a method (not a swappable field), no lock is needed and
// MULTI / active-expire / normal commands all share the same safe path.
func (db *DB) addAof(line CmdLine) {
	if buf, ok := aofBuffers.Load(goid()); ok {
		*buf.(*[]CmdLine) = append(*buf.(*[]CmdLine), line)
		return
	}
	if db.persister != nil {
		db.persister(line)
	}
}

// ExecFunc is interface for command executor
// args don't include cmd line
type ExecFunc func(db *DB, args [][]byte) redis.Reply

// PreFunc analyses command line when queued command to `multi`
// returns related write keys and read keys
type PreFunc func(args [][]byte) ([]string, []string)

// CmdLine is alias for [][]byte, represents a command line
type CmdLine = [][]byte

// UndoFunc returns undo logs for the given command line
// execute from head to tail when undo
type UndoFunc func(db *DB, args [][]byte) []CmdLine

// makeDB create DB instance
func makeDB() *DB {
	db := &DB{
		data:       dict.MakeDict(),
		ttlMap:     dict.MakeConcurrent(ttlDictSize),
		versionMap: dict.MakeConcurrent(dataDictSize),
		lruMap:     dict.MakeConcurrent(dataDictSize),
	}
	return db
}

// makeBasicDB create DB instance only with basic abilities.
func makeBasicDB() *DB {
	db := &DB{
		data:       dict.MakeConcurrent(dataDictSize),
		ttlMap:     dict.MakeConcurrent(ttlDictSize),
		versionMap: dict.MakeConcurrent(dataDictSize),
		lruMap:     dict.MakeConcurrent(dataDictSize),
	}
	return db
}

// Exec executes command within one database
func (db *DB) Exec(c redis.Connection, cmdLine [][]byte) redis.Reply {
	// transaction control commands and other commands which cannot execute within transaction
	cmdName := strings.ToLower(string(cmdLine[0]))
	if cmdName == "multi" {
		if len(cmdLine) != 1 {
			return protocol.MakeArgNumErrReply(cmdName)
		}
		return StartMulti(c)
	} else if cmdName == "discard" {
		if len(cmdLine) != 1 {
			return protocol.MakeArgNumErrReply(cmdName)
		}
		return DiscardMulti(c)
	} else if cmdName == "exec" {
		if len(cmdLine) != 1 {
			return protocol.MakeArgNumErrReply(cmdName)
		}
		return execMulti(db, c)
	} else if cmdName == "watch" {
		if !validateArity(-2, cmdLine) {
			return protocol.MakeArgNumErrReply(cmdName)
		}
		return Watch(db, c, cmdLine[1:])
	}
	if c != nil && c.InMultiState() {
		return EnqueueCmd(c, cmdLine)
	}

	return db.execNormalCommand(c, cmdLine)
}

func (db *DB) execNormalCommand(c redis.Connection, cmdLine [][]byte) redis.Reply {
	cmdName := strings.ToLower(string(cmdLine[0]))
	cmd, ok := cmdTable[cmdName]
	if !ok {
		return protocol.MakeErrReply("ERR unknown command '" + cmdName + "'")
	}
	if !validateArity(cmd.arity, cmdLine) {
		return protocol.MakeArgNumErrReply(cmdName)
	}

	prepare := cmd.prepare
	write, read := prepare(cmdLine[1:])

	// --- denyoom path (global memory critical section) ---
	// maxmemory is a server-wide constraint.  memMu serialises the
	// pre-check → execute sequence so that concurrent denyoom writes
	// on different keys cannot both pass the pre-check.
	// Redis only does pre-check (evict-or-reject); no post-check rollback.
	isDenyOOMCmd := isDenyOOM(cmd) && db.server != nil &&
		config.Properties.Maxmemory > 0

	if isDenyOOMCmd {
		db.server.memMu.Lock()
		defer db.server.memMu.Unlock()

		// Pre-check: evict if possible, reject if still over limit.
		if !db.server.freeMemoryIfNeeded() {
			return oomErrReply()
		}

		db.RWLocks(write, read)
		defer db.RWUnLocks(write, read)
		db.addVersion(write...)

		result := cmd.executor(db, cmdLine[1:])

		if cmd.extra != nil && cmd.extra.notifyClass != 0 {
			if !protocol.IsErrorReply(result) {
				for _, key := range write {
					db.server.notifyKeyspaceEvent(db.index, cmd.extra.notifyClass, cmd.extra.notifyEvent, key)
				}
			}
		}

		// CLIENT TRACKING: track read keys, notify on writes
		if c != nil && !protocol.IsErrorReply(result) {
			if c.IsTracking() && len(write) == 0 && len(read) > 0 {
				trackReadKeys(c, read...)
			}
			if len(write) > 0 {
				notifyWriteKeys(db.server, write, c.ClientID())
			}
		}

		if c != nil && c.Protocol() == redis.RESP3 && cmdName == "hgetall" {
			if mbr, ok := result.(*protocol.MultiBulkReply); ok {
				return convertMultiBulkToMapReply(mbr)
			}
		}
		return result
	}

	// --- Non-denyoom path (no global lock needed) ---
	db.RWLocks(write, read)
	defer db.RWUnLocks(write, read)
	db.addVersion(write...)

	result := cmd.executor(db, cmdLine[1:])

	if cmd.extra != nil && cmd.extra.notifyClass != 0 {
		if !protocol.IsErrorReply(result) {
			for _, key := range write {
				db.server.notifyKeyspaceEvent(db.index, cmd.extra.notifyClass, cmd.extra.notifyEvent, key)
			}
		}
	}

	// CLIENT TRACKING: track read keys, notify on writes
	if c != nil && !protocol.IsErrorReply(result) {
		if c.IsTracking() && len(write) == 0 && len(read) > 0 {
			trackReadKeys(c, read...)
		}
		if len(write) > 0 {
			notifyWriteKeys(db.server, write, c.ClientID())
		}
	}

	if c != nil && c.Protocol() == redis.RESP3 && cmdName == "hgetall" {
		if mbr, ok := result.(*protocol.MultiBulkReply); ok {
			return convertMultiBulkToMapReply(mbr)
		}
	}
	return result
}

// execWithLock executes normal commands, invoker should provide locks
func (db *DB) execWithLock(cmdLine [][]byte) redis.Reply {
	cmdName := strings.ToLower(string(cmdLine[0]))
	cmd, ok := cmdTable[cmdName]
	if !ok {
		return protocol.MakeErrReply("ERR unknown command '" + cmdName + "'")
	}
	if !validateArity(cmd.arity, cmdLine) {
		return protocol.MakeArgNumErrReply(cmdName)
	}
	fun := cmd.executor
	return fun(db, cmdLine[1:])
}

func validateArity(arity int, cmdArgs [][]byte) bool {
	argNum := len(cmdArgs)
	if arity >= 0 {
		return argNum == arity
	}
	return argNum >= -arity
}

// convertMultiBulkToMapReply converts a flat MultiBulkReply with alternating
// key-value pairs into a RESP3 MapReply (%-prefixed). Used by HGETALL when the
// client is speaking RESP3.
func convertMultiBulkToMapReply(mbr *protocol.MultiBulkReply) redis.Reply {
	args := mbr.Args
	if len(args)%2 != 0 {
		return mbr // odd-length, leave as-is
	}
	pairs := make([]redis.Reply, len(args))
	for i, b := range args {
		pairs[i] = protocol.MakeBulkReply(b)
	}
	return resp3.MakeMapReply(pairs)
}

/* ---- Data Access ----- */

// GetEntity returns DataEntity bind to given key
func (db *DB) GetEntity(key string) (*database.DataEntity, bool) {
	raw, ok := db.data.GetWithLock(key)
	if !ok {
		return nil, false
	}
	if db.IsExpired(key) {
		return nil, false
	}
	entity, _ := raw.(*database.DataEntity)
	db.touchLRU(key)
	return entity, true
}

// getEntityNoTouch is like GetEntity but does not update access metadata.
// Used by OBJECT FREQ/IDLETIME (Redis LOOKUP_NOTOUCH).
func (db *DB) getEntityNoTouch(key string) (*database.DataEntity, bool) {
	raw, ok := db.data.GetWithLock(key)
	if !ok {
		return nil, false
	}
	if db.IsExpired(key) {
		return nil, false
	}
	entity, _ := raw.(*database.DataEntity)
	return entity, true
}

// PutEntity a DataEntity into DB
func (db *DB) PutEntity(key string, entity *database.DataEntity) int {
	ret := db.data.PutWithLock(key, entity)
	db.touchLRU(key)
	// db.insertCallback may be set as nil, during `if` and actually callback
	// so introduce a local variable `cb`
	if cb := db.insertCallback; ret > 0 && cb != nil {
		cb(db.index, key, entity)
	}
	return ret
}

// PutIfExists edit an existing DataEntity
func (db *DB) PutIfExists(key string, entity *database.DataEntity) int {
	return db.data.PutIfExistsWithLock(key, entity)
}

// PutIfAbsent insert an DataEntity only if the key not exists
func (db *DB) PutIfAbsent(key string, entity *database.DataEntity) int {
	ret := db.data.PutIfAbsentWithLock(key, entity)
	// db.insertCallback may be set as nil, during `if` and actually callback
	// so introduce a local variable `cb`
	if cb := db.insertCallback; ret > 0 && cb != nil {
		cb(db.index, key, entity)
	}
	return ret
}

// Remove the given key from db
func (db *DB) Remove(key string) {
	raw, deleted := db.data.RemoveWithLock(key)
	db.ttlMap.Remove(key)
	db.lruMap.Remove(key)
	taskKey := genExpireTask(key)
	timewheel.Cancel(taskKey)
	if cb := db.deleteCallback; cb != nil {
		var entity *database.DataEntity
		if deleted > 0 {
			entity = raw.(*database.DataEntity)
		}
		cb(db.index, key, entity)
	}
}

// Removes the given keys from db
func (db *DB) Removes(keys ...string) (deleted int) {
	deleted = 0
	for _, key := range keys {
		_, exists := db.data.GetWithLock(key)
		if exists {
			db.Remove(key)
			deleted++
		}
	}
	return deleted
}

// Flush clean database
// deprecated
// for test only
func (db *DB) Flush() {
	db.data.Clear()
	db.ttlMap.Clear()
}

/* ---- Lock Function ----- */

// RWLocks lock keys for writing and reading
func (db *DB) RWLocks(writeKeys []string, readKeys []string) {
	db.data.RWLocks(writeKeys, readKeys)
}

// RWUnLocks unlock keys for writing and reading
func (db *DB) RWUnLocks(writeKeys []string, readKeys []string) {
	db.data.RWUnLocks(writeKeys, readKeys)
}

/* ---- TTL Functions ---- */

func genExpireTask(key string) string {
	return "expire:" + key
}

// Expire sets ttlCmd of key
func (db *DB) Expire(key string, expireTime time.Time) {
	db.ttlMap.Put(key, expireTime)
	taskKey := genExpireTask(key)
	timewheel.At(expireTime, taskKey, func() {
		keys := []string{key}
		db.RWLocks(keys, nil)
		defer db.RWUnLocks(keys, nil)
		// check-lock-check, ttl may be updated during waiting lock
		logger.Info("expire " + key)
		rawExpireTime, ok := db.ttlMap.Get(key)
		if !ok {
			return
		}
		expireTime, _ := rawExpireTime.(time.Time)
		expired := time.Now().After(expireTime)
		if expired {
			db.Remove(key)
		}
	})
}

// Persist cancel ttlCmd of key
func (db *DB) Persist(key string) {
	db.ttlMap.Remove(key)
	taskKey := genExpireTask(key)
	timewheel.Cancel(taskKey)
}

// IsExpired check whether a key is expired
func (db *DB) IsExpired(key string) bool {
	rawExpireTime, ok := db.ttlMap.Get(key)
	if !ok {
		return false
	}
	expireTime, _ := rawExpireTime.(time.Time)
	expired := time.Now().After(expireTime)
	if expired {
		db.Remove(key)
	}
	return expired
}

/* --- add version --- */

func (db *DB) addVersion(keys ...string) {
	for _, key := range keys {
		versionCode := db.GetVersion(key)
		db.versionMap.Put(key, versionCode+1)
	}
}

// removeVersion deletes the version entry for key (used during OOM rollback
// for keys that did not exist before the failed command).
func (db *DB) removeVersion(key string) {
	db.versionMap.Remove(key)
}

// GetVersion returns version code for given key
func (db *DB) GetVersion(key string) uint32 {
	entity, ok := db.versionMap.Get(key)
	if !ok {
		return 0
	}
	return entity.(uint32)
}

// ForEach traverses all the keys in the database
func (db *DB) ForEach(cb func(key string, data *database.DataEntity, expiration *time.Time) bool) {
	db.data.ForEach(func(key string, raw interface{}) bool {
		entity, _ := raw.(*database.DataEntity)
		var expiration *time.Time
		rawExpireTime, ok := db.ttlMap.Get(key)
		if ok {
			expireTime, _ := rawExpireTime.(time.Time)
			expiration = &expireTime
		}

		return cb(key, entity, expiration)
	})
}
