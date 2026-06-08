package database

import (
	"fmt"
	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/net/goroutine/tcp"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"os"
	"runtime"
	"strings"
	"time"
)

// Ping the server
func Ping(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) == 0 {
		return &protocol.PongReply{}
	} else if len(args) == 1 {
		return protocol.MakeStatusReply(string(args[0]))
	} else {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'ping' command")
	}
}

// Info the information of the godis server returned by the INFO command
func Info(db *Server, args [][]byte) redis.Reply {
	if len(args) == 0 {
		infoCommandList := [...]string{"server", "clients", "memory", "persistence", "replication", "cluster", "keyspace"}
		var allSection []byte
		for _, s := range infoCommandList {
			allSection = append(allSection, GenGodisInfoString(s, db)...)
		}
		return protocol.MakeBulkReply(allSection)
	} else if len(args) == 1 {
		section := strings.ToLower(string(args[0]))
		switch section {
		case "server":
			reply := GenGodisInfoString("server", db)
			return protocol.MakeBulkReply(reply)
		case "client", "clients":
			return protocol.MakeBulkReply(GenGodisInfoString("clients", db))
		case "memory":
			return protocol.MakeBulkReply(GenGodisInfoString("memory", db))
		case "persistence":
			return protocol.MakeBulkReply(GenGodisInfoString("persistence", db))
		case "replication":
			return protocol.MakeBulkReply(GenGodisInfoString("replication", db))
		case "cluster":
			return protocol.MakeBulkReply(GenGodisInfoString("cluster", db))
		case "keyspace":
			return protocol.MakeBulkReply(GenGodisInfoString("keyspace", db))
		case "commandstats":
			return protocol.MakeBulkReply([]byte(db.infoCommandstats()))
		case "latencystats":
			return protocol.MakeBulkReply([]byte(db.infoLatencystats()))
		case "errorstats":
			return protocol.MakeBulkReply([]byte(db.infoErrorstats()))
		case "everything":
			infoCommandList := [...]string{"server", "clients", "memory", "persistence", "replication", "cluster", "keyspace", "commandstats", "latencystats", "errorstats"}
			var allSection []byte
			for _, s := range infoCommandList {
				if s == "commandstats" || s == "latencystats" || s == "errorstats" {
					switch s {
					case "commandstats":
						allSection = append(allSection, db.infoCommandstats()...)
					case "latencystats":
						allSection = append(allSection, db.infoLatencystats()...)
					case "errorstats":
						allSection = append(allSection, db.infoErrorstats()...)
					}
				} else {
					allSection = append(allSection, GenGodisInfoString(s, db)...)
				}
			}
			return protocol.MakeBulkReply(allSection)
		default:
			return protocol.MakeErrReply("Invalid section for 'info' command")
		}
	}
	return protocol.MakeArgNumErrReply("info")
}

// Auth validate client's password
func Auth(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) == 1 {
		// Legacy 1-arg AUTH: authenticate as "default" user
		if config.Properties.RequirePass == "" {
			// No password configured at all — reject AUTH
			return protocol.MakeErrReply("ERR Client sent AUTH, but no password is set")
		}
		passwd := string(args[0])
		c.SetPassword(passwd)
		// Legacy single-password check (overrides ACL nopass)
		if config.Properties.RequirePass != passwd {
			return protocol.MakeErrReply("ERR invalid password")
		}
		// Wire user through ACL if available
		if globalACL != nil {
			u, _ := globalACL.GetUser("default")
			if u != nil {
				c.SetUser(u)
			}
		}
		return &protocol.OkReply{}
	}
	if len(args) == 2 {
		// 2-arg AUTH: username password
		username := string(args[0])
		password := string(args[1])
		c.SetPassword(password)
		if globalACL != nil {
			u, err := globalACL.Authenticate(username, password)
			if err != nil {
				return protocol.MakeErrReply(err.Error())
			}
			c.SetUser(u)
			return &protocol.OkReply{}
		}
		// Fallback: only "default" with RequirePass
		if username == "default" && config.Properties.RequirePass != "" {
			if config.Properties.RequirePass != password {
				return protocol.MakeErrReply("WRONGPASS invalid username-password pair or user is disabled.")
			}
			return &protocol.OkReply{}
		}
		return protocol.MakeErrReply("WRONGPASS invalid username-password pair or user is disabled.")
	}
	return protocol.MakeErrReply("ERR wrong number of arguments for 'auth' command")
}

func isAuthenticated(c redis.Connection) bool {
	if config.Properties.RequirePass == "" {
		return true
	}
	return c.GetPassword() == config.Properties.RequirePass
}

func DbSize(c redis.Connection, db *Server) redis.Reply {
	keys, _ := db.GetDBSize(c.GetDBIndex())
	return protocol.MakeIntReply(int64(keys))
}

func GenGodisInfoString(section string, db *Server) []byte {
	startUpTimeFromNow := getGodisRuninngTime()
	switch section {
	case "server":
		s := fmt.Sprintf("# Server\r\n"+
			"redis_version:%s\r\n"+
			"godis_version:%s\r\n"+
			//"godis_git_sha1:%s\r\n"+
			//"godis_git_dirty:%d\r\n"+
			//"godis_build_id:%s\r\n"+
			"godis_mode:%s\r\n"+
			"os:%s %s\r\n"+
			"arch_bits:%d\r\n"+
			//"multiplexing_api:%s\r\n"+
			"go_version:%s\r\n"+
			"process_id:%d\r\n"+
			"run_id:%s\r\n"+
			"tcp_port:%d\r\n"+
			"uptime_in_seconds:%d\r\n"+
			"uptime_in_days:%d\r\n"+
			"io_threads_active:%d\r\n"+
			//"hz:%d\r\n"+
			//"lru_clock:%d\r\n"+
			"config_file:%s\r\n",
			"8.0.0",
			godisVersion,
			//TODO,
			//TODO,
			//TODO,
			getGodisRunningMode(),
			runtime.GOOS, runtime.GOARCH,
			32<<(^uint(0)>>63),
			//TODO,
			runtime.Version(),
			os.Getpid(),
			config.Properties.RunID,
			config.Properties.Port,
			startUpTimeFromNow,
			startUpTimeFromNow/time.Duration(3600*24),
			0,
			//TODO,
			//TODO,
			config.GetConfigFilePath())
		return []byte(s)
	case "clients":
		s := fmt.Sprintf("# Clients\r\n"+
			"connected_clients:%d\r\n",
			//"client_recent_max_input_buffer:%d\r\n"+
			//"client_recent_max_output_buffer:%d\r\n"+
			//"blocked_clients:%d\n",
			tcp.ClientCounter,
			//TODO,
			//TODO,
			//TODO,
		)
		return []byte(s)
	case "memory":
		s := fmt.Sprintf("# Memory\r\n"+
			"maxmemory:%d\r\n"+
			"maxmemory_policy:%s\r\n"+
			"maxmemory_samples:%d\r\n",
			config.Properties.Maxmemory,
			config.Properties.MaxmemoryPolicy,
			config.Properties.MaxmemorySamples,
		)
		return []byte(s)
	case "persistence":
		aofEnabled := 0
		if config.Properties.AppendOnly {
			aofEnabled = 1
		}
		s := fmt.Sprintf("# Persistence\r\n"+
			"loading:%d\r\n"+
			"rdb_bgsave_in_progress:%d\r\n"+
			"aof_enabled:%d\r\n"+
			"rdb_last_save_time:%d\r\n",
			0,
			0,
			aofEnabled,
			0,
		)
		return []byte(s)
	case "cluster":
		if getGodisRunningMode() == config.ClusterMode {
			s := fmt.Sprintf("# Cluster\r\n"+
				"cluster_enabled:%s\r\n",
				"1",
			)
			return []byte(s)
		} else {
			s := fmt.Sprintf("# Cluster\r\n"+
				"cluster_enabled:%s\r\n",
				"0",
			)
			return []byte(s)
		}
	case "keyspace":
		dbCount := config.Properties.Databases
		var serv []byte
		for i := 0; i < dbCount; i++ {
			keys, expiresKeys := db.GetDBSize(i)
			if keys != 0 {
				ttlSampleAverage := db.GetAvgTTL(i, 20)
				serv = append(serv, getDbSize(i, keys, expiresKeys, ttlSampleAverage)...)
			}
		}
		prefix := []byte("# Keyspace\r\n")
		keyspaceInfo := append(prefix, serv...)
		return keyspaceInfo
	case "replication":
		return genReplicationInfo(db)
	}
	return []byte("")
}

// getGodisRunningMode return godis running mode
func getGodisRunningMode() string {
	if config.Properties.ClusterEnable {
		return config.ClusterMode
	} else {
		return config.StandaloneMode
	}
}

// getGodisRuninngTime return the running time of godis
func getGodisRuninngTime() time.Duration {
	return time.Since(config.EachTimeServerInfo.StartUpTime) / time.Second
}

func getDbSize(dbIndex, keys, expiresKeys int, ttl int64) []byte {
	s := fmt.Sprintf("db%d:keys=%d,expires=%d,avg_ttl=%d\r\n",
		dbIndex, keys, expiresKeys, ttl)
	return []byte(s)
}
