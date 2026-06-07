package database

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func execDebug(server *Server, c redis.Connection, args [][]byte) redis.Reply {
	if len(args) == 0 {
		return protocol.MakeArgNumErrReply("debug")
	}
	switch strings.ToUpper(string(args[0])) {
	case "RELOAD":
		if err := server.debugReload(); err != nil {
			return protocol.MakeErrReply("ERR Error trying to reload: " + err.Error())
		}
		return protocol.MakeOkReply()
	case "LOADAOF":
		if err := server.debugLoadAOF(); err != nil {
			return protocol.MakeErrReply("ERR " + err.Error())
		}
		return protocol.MakeOkReply()
	case "DIGEST":
		d := datasetDigest(server)
		return protocol.MakeStatusReply(hex.EncodeToString(d[:]))
	case "DIGEST-VALUE":
		db := server.mustSelectDB(c.GetDBIndex())
		out := make([]redis.Reply, 0, len(args)-1)
		for _, k := range args[1:] {
			entity, exists := db.GetEntity(string(k))
			if !exists {
				out = append(out, &protocol.NullBulkReply{})
				continue
			}
			var expiration *time.Time
			raw, ok := db.ttlMap.Get(string(k))
			if ok {
				t := raw.(time.Time)
				expiration = &t
			}
			d := digestKey(string(k), entity, expiration)
			out = append(out, protocol.MakeStatusReply(hex.EncodeToString(d[:])))
		}
		return protocol.MakeMultiRawReply(out)
	case "OBJECT":
		if len(args) < 2 {
			return protocol.MakeArgNumErrReply("debug")
		}
		return server.debugObject(c, string(args[1]))
	case "SLEEP":
		if len(args) < 2 {
			return protocol.MakeArgNumErrReply("debug")
		}
		secs, err := strconv.ParseFloat(string(args[1]), 64)
		if err != nil {
			return protocol.MakeErrReply("ERR invalid sleep time")
		}
		time.Sleep(time.Duration(secs * float64(time.Second)))
		return protocol.MakeOkReply()
	case "SET-ACTIVE-EXPIRE":
		server.setActiveExpire(len(args) > 1 && string(args[1]) != "0")
		return protocol.MakeOkReply()
	case "QUICKLIST-PACKED-THRESHOLD":
		return protocol.MakeOkReply()
	case "CHANGE-REPL-ID":
		server.changeReplID()
		return protocol.MakeOkReply()
	case "ERROR":
		if len(args) < 2 {
			return protocol.MakeArgNumErrReply("debug")
		}
		return protocol.MakeErrReply(string(args[1]))
	case "PROTOCOL":
		if len(args) < 2 {
			return protocol.MakeArgNumErrReply("debug")
		}
		return server.debugProtocol(c, string(args[1]))
	case "POPULATE":
		return server.debugPopulate(c, args[1:])
	}
	return protocol.MakeErrReply("ERR DEBUG subcommand not supported")
}

// debugReload saves RDB to a temp file then reloads it.
func (server *Server) debugReload() error {
	rdbFilename := fmt.Sprintf("debug-reload-%d.rdb", time.Now().UnixNano())
	if err := server.saveFaithfulRDB(rdbFilename); err != nil {
		return err
	}
	server.flushAll()
	if err := server.loadFaithfulRDB(rdbFilename); err != nil {
		return err
	}
	_ = os.Remove(rdbFilename)
	return nil
}

// debugLoadAOF reloads the AOF file if persistence is enabled.
func (server *Server) debugLoadAOF() error {
	if server.persister == nil {
		return fmt.Errorf("AOF not enabled")
	}
	server.flushAll()
	server.persister.LoadAof(0)
	return nil
}

// setActiveExpire enables or disables the active-expire cron cycle.
func (server *Server) setActiveExpire(enabled bool) {
	server.activeExpireEnabled = enabled
}

// changeReplID regenerates the master replication ID.
func (server *Server) changeReplID() {
	server.masterStatus.mu.Lock()
	defer server.masterStatus.mu.Unlock()
	server.masterStatus.replId = utils.RandHexString(40)
}

// debugObject returns DEBUG OBJECT info for a key, mimicking Redis output.
func (server *Server) debugObject(c redis.Connection, key string) redis.Reply {
	db := server.mustSelectDB(c.GetDBIndex())
	entity, exists := db.GetEntity(key)
	if !exists {
		return &protocol.NullBulkReply{}
	}

	robj, ok := entity.Data.(*object.Robj)
	if !ok {
		return protocol.MakeBulkReply([]byte("Value at:0x0 refcount:1 encoding:go-native serializedlength:0"))
	}

	syncRobjEncoding(robj)
	encName := robj.EncodingName()
	serializedLen := estimateSerializedLength(robj)
	info := fmt.Sprintf("Value at:0x0 refcount:1 encoding:%s serializedlength:%d", encName, serializedLen)
	return protocol.MakeBulkReply([]byte(info))
}

// estimateSerializedLength returns a rough estimate of the serialized length.
func estimateSerializedLength(robj *object.Robj) int {
	switch robj.Type {
	case object.TypeString:
		switch robj.Encoding {
		case object.EncInt:
			v := robj.Ptr.(int64)
			return len(strconv.FormatInt(v, 10))
		case object.EncEmbstr, object.EncRaw:
			return len(robj.Ptr.([]byte))
		}
	case object.TypeList:
		if l, ok := robj.Ptr.(*object.List); ok {
			total := 0
			l.ForEach(func(i int, val interface{}) bool {
				total += len(valueToBytes(val))
				return true
			})
			return total
		}
	case object.TypeHash:
		if h, ok := robj.Ptr.(*object.Hash); ok {
			total := 0
			h.ForEach(func(field string, value interface{}) bool {
				total += len(field) + len(valueToBytes(value))
				return true
			})
			return total
		}
	case object.TypeSet:
		if s, ok := robj.Ptr.(*object.Set); ok {
			total := 0
			s.ForEach(func(member string) bool {
				total += len(member)
				return true
			})
			return total
		}
	case object.TypeZSet:
		if z, ok := robj.Ptr.(*object.ZSet); ok {
			total := 0
			z.ForEach(func(member string, score float64) bool {
				total += len(member) + len(strconv.FormatFloat(score, 'f', -1, 64))
				return true
			})
			return total
		}
	}
	return 0
}

// debugProtocol returns a reply of the requested RESP type (for TCL test helpers).
func (server *Server) debugProtocol(c redis.Connection, typeName string) redis.Reply {
	switch strings.ToUpper(typeName) {
	case "STRING":
		return protocol.MakeBulkReply([]byte("Hello World"))
	case "INTEGER":
		return protocol.MakeIntReply(12345)
	case "DOUBLE":
		return protocol.MakeStatusReply("3.141592653589793")
	case "BIGNUM":
		return protocol.MakeStatusReply("1234567999999999999999999999999999999")
	case "NULL":
		return &protocol.NullBulkReply{}
	case "ARRAY":
		return protocol.MakeMultiBulkReply([][]byte{
			[]byte("1"),
			[]byte("2"),
			[]byte("3"),
		})
	case "SET":
		return protocol.MakeMultiRawReply([]redis.Reply{
			protocol.MakeBulkReply([]byte("1")),
			protocol.MakeBulkReply([]byte("2")),
			protocol.MakeBulkReply([]byte("3")),
		})
	case "MAP":
		return protocol.MakeMultiRawReply([]redis.Reply{
			protocol.MakeBulkReply([]byte("key")),
			protocol.MakeBulkReply([]byte("val")),
		})
	case "TRUE":
		return protocol.MakeIntReply(1)
	case "FALSE":
		return protocol.MakeIntReply(0)
	case "ERR":
		return protocol.MakeErrReply("ERR debug protocol error")
	case "PUSH":
		return protocol.MakeMultiRawReply([]redis.Reply{
			protocol.MakeBulkReply([]byte("message")),
			protocol.MakeBulkReply([]byte("channel")),
			protocol.MakeBulkReply([]byte("data")),
		})
	}
	return protocol.MakeErrReply("ERR Unknown DEBUG PROTOCOL type: " + typeName)
}

// debugPopulate performs DEBUG POPULATE count [prefix [size]].
func (server *Server) debugPopulate(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeArgNumErrReply("debug")
	}
	count, err := strconv.Atoi(string(args[0]))
	if err != nil || count <= 0 {
		return protocol.MakeErrReply("ERR invalid count")
	}
	prefix := "key:"
	if len(args) > 1 {
		prefix = string(args[1])
	}
	valSize := 0
	if len(args) > 2 {
		valSize, _ = strconv.Atoi(string(args[2]))
	}

	val := make([]byte, valSize)
	for i := range val {
		val[i] = 'x'
	}

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s%d", prefix, i)
		cmdLine := utils.ToCmdLine("SET", key, string(val))
		server.Exec(c, cmdLine)
	}
	return protocol.MakeOkReply()
}
