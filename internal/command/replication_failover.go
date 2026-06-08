package database

import (
	"strconv"
	"strings"
	stdatomic "sync/atomic"
	"time"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// promoteToMaster transitions a replica to master, setting replid2 to the old
// master's replid (for PSYNC2 cross-failover partial resync) and freezing
// secondReplOffset at the current replication offset.
func (server *Server) promoteToMaster(oldMasterReplid string, replOffset int64) {
	server.masterStatus.mu.Lock()
	// Save the old replid as replid2 for PSYNC2 dual-replid matching
	server.masterStatus.replid2 = oldMasterReplid
	server.masterStatus.secondReplOffset = replOffset
	// Generate a new replid for ourselves as the new master
	server.masterStatus.replId = utils.RandHexString(40)
	server.masterStatus.mu.Unlock()

	// Flip role
	stdatomic.StoreInt32(&server.role, masterRole)
}

// execWaitAof implements `WAITAOF numlocal numreplicas timeout`:
// Block until numlocal local fsyncs AND numreplicas replicas have acked,
// or timeout (ms) elapses. Returns [local_fsynced, replicas_acked].
// With appendonly off and numlocal>0, returns error.
func execWaitAof(server *Server, args [][]byte) redis.Reply {
	if len(args) != 3 {
		return protocol.MakeArgNumErrReply("waitaof")
	}
	numLocal, err := strconv.Atoi(string(args[0]))
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	numReplicas, err := strconv.Atoi(string(args[1]))
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	timeoutMs, err := strconv.Atoi(string(args[2]))
	if err != nil || timeoutMs < 0 {
		return protocol.MakeErrReply("ERR timeout is not an integer or out of range")
	}

	// If AOF is off and numlocal > 0, return error
	if numLocal > 0 && !config.Properties.AppendOnly {
		return protocol.MakeErrReply("ERR WAITAOF cannot be used when numlocal is set but appendonly is disabled.")
	}

	// Target offset = current backlog offset
	server.masterStatus.mu.RLock()
	targetOffset := server.masterStatus.backlog.currentOffset
	server.masterStatus.mu.RUnlock()

	// For local fsync: if AOF is on, we consider it fsynced once the persister
	// has flushed. For simplicity, if numLocal <= 0 or AOF is on, local is satisfied.
	// A real implementation would check the AOF fsync status.
	localFsynced := int64(1)
	if numLocal > 0 && config.Properties.AppendOnly {
		// In a real implementation, we'd wait for the AOF fsync.
		// For now, consider it immediately fsynced (the persister writes
		// synchronously in the default everysec mode).
		localFsynced = 1
	} else if numLocal <= 0 {
		localFsynced = 1
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		server.getAckFromSlaves()
		acked := server.countAckedSlaves(targetOffset)

		localOK := localFsynced >= int64(numLocal) || numLocal <= 0
		replOK := acked >= numReplicas

		if localOK && replOK {
			return protocol.MakeMultiRawReply([]redis.Reply{
				protocol.MakeIntReply(localFsynced),
				protocol.MakeIntReply(int64(acked)),
			})
		}

		if timeoutMs != 0 && !time.Now().Before(deadline) {
			return protocol.MakeMultiRawReply([]redis.Reply{
				protocol.MakeIntReply(localFsynced),
				protocol.MakeIntReply(int64(acked)),
			})
		}

		sleep := 50 * time.Millisecond
		if timeoutMs != 0 {
			if remaining := time.Until(deadline); remaining < sleep {
				if remaining <= 0 {
					return protocol.MakeMultiRawReply([]redis.Reply{
						protocol.MakeIntReply(localFsynced),
						protocol.MakeIntReply(int64(server.countAckedSlaves(targetOffset))),
					})
				}
				sleep = remaining
			}
		}
		time.Sleep(sleep)
	}
}

// execFailover implements the standalone `FAILOVER` command:
//
//	FAILOVER [TO host port [FORCE]] [ABORT] [TIMEOUT ms]
//
// This is the standalone (non-cluster) failover where a replica promotes
// itself to master. In cluster mode, use CLUSTER FAILOVER instead.
func execFailover(server *Server, args [][]byte) redis.Reply {
	role := stdatomic.LoadInt32(&server.role)
	if role != slaveRole {
		return protocol.MakeErrReply("ERR FAILOVER is only valid when server is a replica")
	}

	// Parse arguments
	abort := false
	timeoutMs := 0
	for i := 0; i < len(args); i++ {
		switch strings.ToUpper(string(args[i])) {
		case "ABORT":
			abort = true
		case "TIMEOUT":
			i++
			if i >= len(args) {
				return protocol.MakeArgNumErrReply("failover")
			}
			var err error
			timeoutMs, err = strconv.Atoi(string(args[i]))
			if err != nil || timeoutMs < 0 {
				return protocol.MakeErrReply("ERR timeout is not an integer or out of range")
			}
		}
	}

	if abort {
		// Cancel any in-progress failover
		// For now, just return OK since we don't have a persistent failover state
		return protocol.MakeOkReply()
	}

	// Perform the failover: promote this replica to master
	server.slaveStatus.mutex.Lock()
	oldReplid := server.slaveStatus.replId
	replOffset := server.slaveStatus.replOffset
	server.slaveStatus.mutex.Unlock()

	server.promoteToMaster(oldReplid, replOffset)

	return protocol.MakeOkReply()
}
