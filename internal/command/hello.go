package database

import (
	"strconv"
	"strings"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/proto/resp3"
)

// execHello negotiates RESP version and returns the handshake map.
// Syntax: HELLO [protover [AUTH user pass] [SETNAME name]]
func execHello(conn redis.Connection, args [][]byte) redis.Reply {
	if len(args) >= 1 {
		ver, err := strconv.Atoi(string(args[0]))
		if err != nil || (ver != 2 && ver != 3) {
			return protocol.MakeErrReply("NOPROTO unsupported protocol version")
		}
		conn.SetProtocol(redis.RespVersion(ver))
	}
	i := 1
	for i < len(args) {
		switch strings.ToUpper(string(args[i])) {
		case "AUTH":
			if i+2 >= len(args) {
				return protocol.MakeErrReply("ERR syntax error in HELLO")
			}
			_ = string(args[i+1]) // username ignored in Redis
			passwd := string(args[i+2])
			if config.Properties.RequirePass == "" {
				return protocol.MakeErrReply("ERR Client sent AUTH, but no password is set")
			}
			// Redis HELLO AUTH ignores the username; only password matters.
			if config.Properties.RequirePass != passwd {
				return protocol.MakeErrReply("WRONGPASS invalid username-password pair or user is disabled.")
			}
			conn.SetPassword(passwd)
			i += 3
		case "SETNAME":
			if i+1 >= len(args) {
				return protocol.MakeErrReply("ERR syntax error in HELLO")
			}
			i += 2
		default:
			return protocol.MakeErrReply("ERR syntax error in HELLO")
		}
	}
	return helloReply(conn)
}

func helloReply(conn redis.Connection) redis.Reply {
	pairs := []redis.Reply{
		protocol.MakeBulkReply([]byte("server")), protocol.MakeBulkReply([]byte("hayakv")),
		protocol.MakeBulkReply([]byte("version")), protocol.MakeBulkReply([]byte("8.0.0")),
		protocol.MakeBulkReply([]byte("proto")), protocol.MakeIntReply(int64(conn.Protocol())),
		protocol.MakeBulkReply([]byte("id")), protocol.MakeIntReply(1),
		protocol.MakeBulkReply([]byte("mode")), protocol.MakeBulkReply([]byte("standalone")),
		protocol.MakeBulkReply([]byte("role")), protocol.MakeBulkReply([]byte("master")),
		protocol.MakeBulkReply([]byte("modules")), protocol.MakeEmptyMultiBulkReply(),
	}
	if conn.Protocol() == redis.RESP3 {
		return resp3.MakeMapReply(pairs)
	}
	return protocol.MakeMultiRawReply(pairs)
}
