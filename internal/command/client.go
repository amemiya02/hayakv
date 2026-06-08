package database

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func execClient(server *Server, c redis.Connection, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'client' command")
	}
	subCmd := strings.ToLower(string(args[0]))
	switch subCmd {
	case "id":
		return clientID(c)
	case "getname":
		return clientGetName(c)
	case "setname":
		return clientSetName(c, args[1:])
	case "setinfo":
		return clientSetInfo(c, args[1:])
	case "info":
		return clientInfo(c)
	case "list":
		return clientList(args[1:])
	case "kill":
		return clientKill(args[1:])
	case "reply":
		return clientReply(c, args[1:])
	case "tracking":
		return execClientTracking(server, c, args[1:])
	case "trackinginfo":
		return clientTrackingInfo(c)
	case "getredirect":
		return clientGetRedirect(c)
	case "caching":
		return clientCaching(c, args[1:])
	default:
		return protocol.MakeErrReply("ERR unknown subcommand '" + string(args[0]) + "'")
	}
}

func clientID(c redis.Connection) redis.Reply {
	return protocol.MakeIntReply(int64(c.ClientID()))
}

func clientGetName(c redis.Connection) redis.Reply {
	name := c.ClientName()
	if name == "" {
		return protocol.MakeNullBulkReply()
	}
	return protocol.MakeBulkReply([]byte(name))
}

func clientSetName(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) != 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'client|setname' command")
	}
	name := string(args[0])
	if strings.Contains(name, " ") {
		return protocol.MakeErrReply("ERR Client names cannot contain spaces")
	}
	c.SetClientName(name)
	return protocol.MakeOkReply()
}

func clientSetInfo(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) != 2 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'client|setinfo' command")
	}
	key := strings.ToLower(string(args[0]))
	val := string(args[1])
	switch key {
	case "lib-name":
		c.SetLibName(val)
	case "lib-ver":
		c.SetLibVer(val)
	default:
		return protocol.MakeErrReply("ERR Unsupported option '" + string(args[0]) + "'")
	}
	return protocol.MakeOkReply()
}

func formatClientInfo(c redis.Connection) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("id=%d", c.ClientID()))

	addr := ""
	if c.RemoteAddr() != "" {
		addr = c.RemoteAddr()
	}
	sb.WriteString(fmt.Sprintf(" addr=%s", addr))
	sb.WriteString(" laddr= fd=0")

	name := c.ClientName()
	if name == "" {
		name = ""
	}
	sb.WriteString(fmt.Sprintf(" name=%s", name))

	age := int64(time.Since(c.CreatedAt()).Seconds())
	sb.WriteString(fmt.Sprintf(" age=%d", age))
	sb.WriteString(" idle=0")

	flags := "N"
	if c.IsSlave() {
		flags = "S"
	}
	if c.IsMaster() {
		flags = "M"
	}
	sb.WriteString(fmt.Sprintf(" flags=%s", flags))

	sb.WriteString(fmt.Sprintf(" db=%d", c.GetDBIndex()))
	sb.WriteString(fmt.Sprintf(" sub=%d", c.SubsCount()))
	sb.WriteString(fmt.Sprintf(" psub=%d", c.PatternCount()))

	resp := "2"
	if c.Protocol() == redis.RESP3 {
		resp = "3"
	}
	sb.WriteString(fmt.Sprintf(" resp=%s", resp))

	sb.WriteString(fmt.Sprintf(" lib-name=%s", c.LibName()))
	sb.WriteString(fmt.Sprintf(" lib-ver=%s", c.LibVer()))

	return sb.String()
}

func clientInfo(c redis.Connection) redis.Reply {
	info := formatClientInfo(c)
	return protocol.MakeBulkReply([]byte(info))
}

func clientList(args [][]byte) redis.Reply {
	clients := connection.AllClients()
	var lines []string
	for _, cc := range clients {
		lines = append(lines, formatClientInfo(cc))
	}
	result := strings.Join(lines, "\n") + "\n"
	return protocol.MakeBulkReply([]byte(result))
}

func clientKill(args [][]byte) redis.Reply {
	if len(args) == 0 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'client|kill' command")
	}

	// Parse options: CLIENT KILL [ID <id>] [ADDR <addr>] [LADDR <laddr>] [SKIPME yes|no]
	var filterID uint64
	var filterAddr string
	var filterLAddr string
	var hasID bool
	skipMe := true

	i := 0
	for i < len(args) {
		opt := strings.ToUpper(string(args[i]))
		switch opt {
		case "ID":
			if i+1 >= len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			id, err := strconv.ParseUint(string(args[i+1]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR Invalid client ID")
			}
			filterID = id
			hasID = true
			i += 2
		case "ADDR":
			if i+1 >= len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			filterAddr = string(args[i+1])
			i += 2
		case "LADDR":
			if i+1 >= len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			filterLAddr = string(args[i+1])
			i += 2
		case "SKIPME":
			if i+1 >= len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			skipMe = strings.ToLower(string(args[i+1])) != "no"
			i += 2
		default:
			// Old-style: CLIENT KILL <addr>
			if i == 0 && len(args) == 1 {
				filterAddr = string(args[0])
				i++
			} else {
				return protocol.MakeErrReply("ERR syntax error")
			}
		}
	}

	killed := 0
	clients := connection.AllClients()
	for _, cc := range clients {
		match := false
		if hasID && cc.ClientID() == filterID {
			match = true
		}
		if filterAddr != "" {
			if cc.RemoteAddr() == filterAddr {
				match = true
			}
		}
		if filterLAddr != "" {
			// laddr not tracked yet, skip
		}
		if match {
			if skipMe && cc.ClientID() == 0 {
				continue
			}
			cc.Close()
			killed++
		}
	}

	return protocol.MakeIntReply(int64(killed))
}

func clientReply(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) != 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'client|reply' command")
	}
	mode := strings.ToLower(string(args[0]))
	switch mode {
	case "on":
		c.SetReplyMode(0)
	case "off":
		c.SetReplyMode(1)
	case "skip":
		c.SetReplyMode(2)
	default:
		return protocol.MakeErrReply("ERR syntax error")
	}
	return protocol.MakeOkReply()
}
