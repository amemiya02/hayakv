package database

import (
	"strings"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// Watch set watching keys
func Watch(db *DB, conn redis.Connection, args [][]byte) redis.Reply {
	watching := conn.GetWatching()
	for _, bkey := range args {
		key := string(bkey)
		watching[key] = db.GetVersion(key)
	}
	return protocol.MakeOkReply()
}

func execGetVersion(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	ver := db.GetVersion(key)
	return protocol.MakeIntReply(int64(ver))
}

func init() {
	registerCommand("GetVer", execGetVersion, readAllKeys, nil, 2, flagReadOnly)
}

// invoker should lock watching keys
func isWatchingChanged(db *DB, watching map[string]uint32) bool {
	for key, ver := range watching {
		currentVersion := db.GetVersion(key)
		if ver != currentVersion {
			return true
		}
	}
	return false
}

// StartMulti starts multi-command-transaction
func StartMulti(conn redis.Connection) redis.Reply {
	if conn.InMultiState() {
		return protocol.MakeErrReply("ERR MULTI calls can not be nested")
	}
	conn.SetMultiState(true)
	return protocol.MakeOkReply()
}

// EnqueueCmd puts command line into `multi` pending queue
func EnqueueCmd(conn redis.Connection, cmdLine [][]byte) redis.Reply {
	cmdName := strings.ToLower(string(cmdLine[0]))
	cmd, ok := cmdTable[cmdName]
	if !ok {
		err := protocol.MakeErrReply("ERR unknown command '" + cmdName + "'")
		conn.AddTxError(err)
		return err
	}
	if cmd.prepare == nil {
		err := protocol.MakeErrReply("ERR command '" + cmdName + "' cannot be used in MULTI")
		conn.AddTxError(err)
		return err
	}
	if !validateArity(cmd.arity, cmdLine) {
		err := protocol.MakeArgNumErrReply(cmdName)
		conn.AddTxError(err)
		return err
	}
	conn.EnqueueCmd(cmdLine)
	return protocol.MakeQueuedReply()
}

func execMulti(db *DB, conn redis.Connection) redis.Reply {
	if !conn.InMultiState() {
		return protocol.MakeErrReply("ERR EXEC without MULTI")
	}
	defer conn.SetMultiState(false)
	if len(conn.GetTxErrors()) > 0 {
		return protocol.MakeErrReply("EXECABORT Transaction discarded because of previous errors.")
	}
	cmdLines := conn.GetQueuedCmdLine()
	return db.ExecMulti(conn, conn.GetWatching(), cmdLines)
}

// ExecMulti executes multi commands transaction Atomically and Isolated
func (db *DB) ExecMulti(conn redis.Connection, watching map[string]uint32, cmdLines []CmdLine) redis.Reply {
	// prepare — collect all write/read keys and check for denyoom commands.
	writeKeys := make([]string, 0) // may contains duplicate
	readKeys := make([]string, 0)
	hasDenyOOM := false
	for _, cmdLine := range cmdLines {
		cmdName := strings.ToLower(string(cmdLine[0]))
		cmd := cmdTable[cmdName]
		prepare := cmd.prepare
		write, read := prepare(cmdLine[1:])
		writeKeys = append(writeKeys, write...)
		readKeys = append(readKeys, read...)
		if isDenyOOM(cmd) {
			hasDenyOOM = true
		}
	}
	// set watch
	watchingKeys := make([]string, 0, len(watching))
	for key := range watching {
		watchingKeys = append(watchingKeys, key)
	}
	readKeys = append(readKeys, watchingKeys...)

	// WATCH check before any locks — versionMap is a ConcurrentDict so
	// reading versions is safe without key locks.  This must happen before
	// the memory pre-check so that a WATCH-aborted transaction is never
	// rejected with a misleading OOM error.
	if isWatchingChanged(db, watching) { // watching keys changed, abort
		return protocol.MakeEmptyMultiBulkReply()
	}

	// M5: global memory critical section for transactions with denyoom
	// commands.  Serialises with concurrent denyoom writes on other keys.
	// Lock ordering: memMu → key locks (same as execNormalCommand).
	isOOMTxn := hasDenyOOM && db.server != nil &&
		config.Properties.Maxmemory > 0 &&
		config.Properties.MaxmemoryPolicy == "noeviction"
	if isOOMTxn {
		db.server.memMu.Lock()
		defer db.server.memMu.Unlock()
		if !db.server.freeMemoryIfNeeded() {
			return oomErrReply()
		}
	}

	db.RWLocks(writeKeys, readKeys)
	defer db.RWUnLocks(writeKeys, readKeys)

	// M5: snapshot write-key metadata for OOM rollback.
	// (undo logs restore data; snapshot restores version + LRU.)
	type keyMeta struct {
		existed    bool
		oldVersion uint32
		oldLRU     lruMeta
		hasLRU     bool
	}
	var metaSnaps map[string]keyMeta
	if isOOMTxn && len(writeKeys) > 0 {
		metaSnaps = make(map[string]keyMeta, len(writeKeys))
		for _, k := range writeKeys {
			m := keyMeta{oldVersion: db.GetVersion(k)}
			if _, ok := db.data.GetWithLock(k); ok {
				m.existed = true
			}
			if raw, ok := db.lruMap.Get(k); ok {
				m.oldLRU = raw.(lruMeta)
				m.hasLRU = true
			}
			metaSnaps[k] = m
		}
	}

	// M5: buffer AOF writes so they can be discarded on OOM rollback.
	var aofBuf []CmdLine
	if isOOMTxn && len(writeKeys) > 0 {
		gid := goid()
		aofBuffers.Store(gid, &aofBuf)
		defer func() { aofBuffers.Delete(gid) }()
	}

	// execute
	results := make([]redis.Reply, 0, len(cmdLines))
	aborted := false
	undoCmdLines := make([][]CmdLine, 0, len(cmdLines))
	isRESP3 := conn != nil && conn.Protocol() == redis.RESP3
	for _, cmdLine := range cmdLines {
		undoCmdLines = append(undoCmdLines, db.GetUndoLogs(cmdLine))
		result := db.execWithLock(cmdLine)
		if protocol.IsErrorReply(result) {
			aborted = true
			// don't rollback failed commands
			undoCmdLines = undoCmdLines[:len(undoCmdLines)-1]
			break
		}
		// RESP3: convert HGETALL array reply to map inside MULTI/EXEC
		if isRESP3 {
			cmdName := strings.ToLower(string(cmdLine[0]))
			if cmdName == "hgetall" {
				if mbr, ok := result.(*protocol.MultiBulkReply); ok {
					result = convertMultiBulkToMapReply(mbr)
				}
			}
		}
		results = append(results, result)
	}

	// M5: post-write OOM check.  The error might be OOM from a denyoom
	// command inside the transaction, or a normal error.  Only do the
	// full OOM rollback when we are in the OOM path AND memory is still
	// over limit (the undo logs handle data; snapshot handles meta).
	if isOOMTxn && db.server.usedMemory() > config.Properties.Maxmemory {
		aborted = true
	}

	if !aborted { // success
		db.addVersion(writeKeys...)
		// Flush buffered AOF to persister.
		if len(aofBuf) > 0 {
			aofBuffers.Delete(goid())
			for _, line := range aofBuf {
				db.addAof(line)
			}
		}
		return protocol.MakeMultiRawReply(results)
	}

	// undo if aborted
	size := len(undoCmdLines)
	for i := size - 1; i >= 0; i-- {
		curCmdLines := undoCmdLines[i]
		if len(curCmdLines) == 0 {
			continue
		}
		for _, cmdLine := range curCmdLines {
			db.execWithLock(cmdLine)
		}
	}

	// M5: restore version + LRU metadata after undo logs restored data.
	if metaSnaps != nil {
		for k, m := range metaSnaps {
			if m.existed {
				db.versionMap.Put(k, m.oldVersion)
			} else {
				db.removeVersion(k)
			}
			if m.hasLRU {
				db.lruMap.Put(k, m.oldLRU)
			} else {
				db.lruMap.Remove(k)
			}
		}
	}

	// Discard buffered AOF entries.
	return protocol.MakeErrReply("EXECABORT Transaction discarded because of previous errors.")
}

// DiscardMulti drops MULTI pending commands
func DiscardMulti(conn redis.Connection) redis.Reply {
	if !conn.InMultiState() {
		return protocol.MakeErrReply("ERR DISCARD without MULTI")
	}
	conn.ClearQueuedCmds()
	conn.SetMultiState(false)
	return protocol.MakeOkReply()
}

// GetUndoLogs return rollback commands
func (db *DB) GetUndoLogs(cmdLine [][]byte) []CmdLine {
	cmdName := strings.ToLower(string(cmdLine[0]))
	cmd, ok := cmdTable[cmdName]
	if !ok {
		return nil
	}
	undo := cmd.undo
	if undo == nil {
		return nil
	}
	return undo(db, cmdLine[1:])
}

// GetRelatedKeys analysis related keys
// returns related write keys and read keys
func GetRelatedKeys(cmdLine [][]byte) ([]string, []string) {
	cmdName := strings.ToLower(string(cmdLine[0]))
	cmd, ok := cmdTable[cmdName]
	if !ok {
		return nil, nil
	}
	prepare := cmd.prepare
	if prepare == nil {
		return nil, nil
	}
	return prepare(cmdLine[1:])
}
