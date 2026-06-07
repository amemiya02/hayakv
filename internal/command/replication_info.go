package database

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// genReplicationInfo builds the `# Replication` INFO section with the fields
// real clients and the redis test suite parse. Volatile values (offsets, replid)
// are emitted verbatim; tests normalize them.
func genReplicationInfo(server *Server) []byte {
	var b strings.Builder
	b.WriteString("# Replication\r\n")
	role := atomic.LoadInt32(&server.role)
	if role == slaveRole {
		b.WriteString("role:slave\r\n")
	} else {
		b.WriteString("role:master\r\n")
	}

	// ---- replica-specific fields ----
	if role == slaveRole {
		repl := server.slaveStatus
		repl.mutex.Lock()
		host := repl.masterHost
		port := repl.masterPort
		linkUp := repl.masterConn != nil
		lastIO := int64(-1)
		if !repl.lastRecvTime.IsZero() {
			lastIO = int64(time.Since(repl.lastRecvTime).Seconds())
		}
		slaveOffset := repl.replOffset
		repl.mutex.Unlock()

		b.WriteString(fmt.Sprintf("master_host:%s\r\n", host))
		b.WriteString(fmt.Sprintf("master_port:%d\r\n", port))
		link := "down"
		if linkUp {
			link = "up"
		}
		b.WriteString(fmt.Sprintf("master_link_status:%s\r\n", link))
		b.WriteString(fmt.Sprintf("master_last_io_seconds_ago:%d\r\n", lastIO))
		b.WriteString("master_sync_in_progress:0\r\n")
		b.WriteString(fmt.Sprintf("slave_read_only:%d\r\n", 1))
		b.WriteString(fmt.Sprintf("slave_repl_offset:%d\r\n", slaveOffset))
	}

	// ---- master view (always emitted, redis emits connected_slaves + slaveN on a master) ----
	server.masterStatus.mu.RLock()
	onlineSlaves := make([]*slaveClient, 0, len(server.masterStatus.onlineSlaves))
	for slave := range server.masterStatus.onlineSlaves {
		onlineSlaves = append(onlineSlaves, slave)
	}
	masterReplId := server.masterStatus.replId
	masterReplOffset := server.masterStatus.backlog.currentOffset
	server.masterStatus.mu.RUnlock()

	b.WriteString(fmt.Sprintf("connected_slaves:%d\r\n", len(onlineSlaves)))
	for i, slave := range onlineSlaves {
		ip := slave.announceIp
		if ip == "" {
			ip = slave.conn.RemoteAddr()
		}
		lag := int64(0)
		if !slave.lastAckTime.IsZero() {
			lag = int64(time.Since(slave.lastAckTime).Seconds())
		}
		b.WriteString(fmt.Sprintf("slave%d:ip=%s,port=%d,state=online,offset=%d,lag=%d\r\n",
			i, ip, slave.announcePort, slave.offset, lag))
	}
	b.WriteString("master_failover_state:no-failover\r\n")
	b.WriteString(fmt.Sprintf("master_replid:%s\r\n", masterReplId))
	b.WriteString("master_replid2:0000000000000000000000000000000000000000\r\n")
	b.WriteString(fmt.Sprintf("master_repl_offset:%d\r\n", masterReplOffset))
	b.WriteString("second_repl_offset:-1\r\n")
	b.WriteString("repl_backlog_active:1\r\n")
	b.WriteString(fmt.Sprintf("repl_backlog_size:%d\r\n", server.masterStatus.backlog.limit))
	b.WriteString(fmt.Sprintf("repl_backlog_first_byte_offset:%d\r\n", server.masterStatus.backlog.beginOffset))
	return []byte(b.String())
}
