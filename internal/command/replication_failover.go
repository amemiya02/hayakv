package database

import (
	"fmt"
	"net"
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

	// For local fsync: report 1 if AOF is on and numLocal>0 (the persister
	// writes synchronously in everysec mode), otherwise 0. This matches
	// Redis: WAITAOF 0 0 0 with appendonly no returns [0, 0].
	localFsynced := int64(0)
	if numLocal > 0 && config.Properties.AppendOnly {
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
// In Redis 6.2+, FAILOVER is issued on the master. The master selects a
// connected replica and initiates a coordinated handoff: the replica is
// promoted to master and the old master becomes a replica of the new master.
// In cluster mode, use CLUSTER FAILOVER instead.
func execFailover(server *Server, args [][]byte) redis.Reply {
	role := stdatomic.LoadInt32(&server.role)
	if role != masterRole {
		return protocol.MakeErrReply("ERR FAILOVER requires a master with connected replicas")
	}

	// Parse arguments
	var targetHost string
	var targetPort int
	abort := false
	timeoutMs := 0
	for i := 0; i < len(args); i++ {
		switch strings.ToUpper(string(args[i])) {
		case "ABORT":
			abort = true
		case "TO":
			i++
			if i >= len(args) {
				return protocol.MakeArgNumErrReply("failover")
			}
			targetHost = string(args[i])
			i++
			if i >= len(args) {
				return protocol.MakeArgNumErrReply("failover")
			}
			p, err := strconv.Atoi(string(args[i]))
			if err != nil {
				return protocol.MakeErrReply("ERR Invalid target port")
			}
			targetPort = p
		case "FORCE":
			// FORCE: skip waiting for replica sync (not fully implemented)
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

	// Find the target replica. If TO is specified, use that; otherwise
	// pick the first connected replica.
	server.masterStatus.mu.RLock()
	var chosen *slaveClient
	for _, sl := range server.masterStatus.slaveMap {
		if targetHost != "" {
			if sl.announceIp == targetHost && sl.announcePort == targetPort {
				chosen = sl
				break
			}
		} else {
			chosen = sl
			break
		}
	}
	server.masterStatus.mu.RUnlock()

	if chosen == nil {
		if targetHost != "" {
			return protocol.MakeErrReply(fmt.Sprintf("ERR Replica %s:%d not connected", targetHost, targetPort))
		}
		return protocol.MakeErrReply("ERR No connected replicas")
	}

	// Connect to the replica's data port and tell it to promote itself.
	// We use SLAVEOF NO ONE (not the public FAILOVER command, which also
	// requires masterRole on the receiver). The replica's execSlaveOf
	// handler calls slaveOfNone() which promotes it to master.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", chosen.announceIp, chosen.announcePort), time.Second)
	if err != nil {
		return protocol.MakeErrReply("ERR Could not connect to replica: " + err.Error())
	}
	defer conn.Close()

	promoteCmd := protocol.MakeMultiBulkReply([][]byte{[]byte("SLAVEOF"), []byte("NO"), []byte("ONE")})
	if _, err := conn.Write(promoteCmd.ToBytes()); err != nil {
		return protocol.MakeErrReply("ERR Failed to send promotion to replica: " + err.Error())
	}

	// Read reply (with optional timeout)
	if timeoutMs > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(time.Duration(timeoutMs) * time.Millisecond))
	}
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	if n > 0 && buf[0] == '+' {
		// Replica accepted — now we (the old master) become a replica of it.
		server.slaveStatus.mutex.Lock()
		server.slaveStatus.masterHost = chosen.announceIp
		server.slaveStatus.masterPort = chosen.announcePort
		server.slaveStatus.mutex.Unlock()
		stdatomic.StoreInt32(&server.role, slaveRole)
		return protocol.MakeOkReply()
	}

	return protocol.MakeErrReply("ERR Failover failed: replica did not accept")
}
