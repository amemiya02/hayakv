package database

import (
	"strconv"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// execWait implements `WAIT numreplicas timeout`: block until at least
// numreplicas replicas have acked the master's current offset, or timeout (ms)
// elapses. Returns the count of replicas that reached the target offset.
func execWait(server *Server, args [][]byte) redis.Reply {
	if len(args) != 2 {
		return protocol.MakeArgNumErrReply("wait")
	}
	numReplicas, err := strconv.Atoi(string(args[0]))
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	timeoutMs, err := strconv.Atoi(string(args[1]))
	if err != nil || timeoutMs < 0 {
		return protocol.MakeErrReply("ERR timeout is not an integer or out of range")
	}

	// Target offset = current backlog offset (everything written so far).
	server.masterStatus.mu.RLock()
	targetOffset := server.masterStatus.backlog.currentOffset
	server.masterStatus.mu.RUnlock()

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		server.getAckFromSlaves()
		acked := server.countAckedSlaves(targetOffset)
		if acked >= numReplicas {
			return protocol.MakeIntReply(int64(acked))
		}
		if timeoutMs != 0 && !time.Now().Before(deadline) {
			return protocol.MakeIntReply(int64(acked))
		}
		// Bounded poll; replicas ack asynchronously via receiveAOF.
		sleep := 50 * time.Millisecond
		if timeoutMs != 0 {
			if remaining := time.Until(deadline); remaining < sleep {
				if remaining <= 0 {
					return protocol.MakeIntReply(int64(server.countAckedSlaves(targetOffset)))
				}
				sleep = remaining
			}
		}
		time.Sleep(sleep)
	}
}

// countAckedSlaves returns how many online slaves have acked at least offset.
func (server *Server) countAckedSlaves(offset int64) int {
	server.masterStatus.mu.RLock()
	defer server.masterStatus.mu.RUnlock()
	count := 0
	for slave := range server.masterStatus.onlineSlaves {
		if slave.offset >= offset {
			count++
		}
	}
	return count
}
