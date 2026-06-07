package aof

import (
	"strconv"
	"time"

	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	List "github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/datastruct/set"
	SortedSet "github.com/amemiya02/hayakv/internal/datastruct/sortedset"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// EntityToCmd serialize data entity to redis command
func EntityToCmd(key string, entity *database.DataEntity) *protocol.MultiBulkReply {
	if entity == nil {
		return nil
	}
	var cmd *protocol.MultiBulkReply
	switch val := entity.Data.(type) {
	case *object.Robj:
		cmd = robjToCmd(key, val)
	case []byte:
		cmd = stringToCmd(key, val)
	case *object.List:
		cmd = objectListToCmd(key, val)
	case *object.ZSet:
		cmd = objectZSetToCmd(key, val)
	case *object.Set:
		cmd = objectSetToCmd(key, val)
	case List.List:
		cmd = listToCmd(key, val)
	case *set.Set:
		cmd = setToCmd(key, val)
	case dict.Dict:
		cmd = hashToCmd(key, val)
	case *SortedSet.SortedSet:
		cmd = zSetToCmd(key, val)
	}
	return cmd
}

// robjToCmd serializes an Robj to the appropriate redis command
func robjToCmd(key string, robj *object.Robj) *protocol.MultiBulkReply {
	switch robj.Type {
	case object.TypeString:
		return stringToCmd(key, robj.GetStringBytes())
	case object.TypeList:
		list, ok := robj.Value().(*object.List)
		if !ok {
			return nil
		}
		return objectListToCmd(key, list)
	case object.TypeSet:
		// Try new encoding-layer Set first, then legacy set.Set
		if objSet, ok := robj.Value().(*object.Set); ok {
			return objectSetToCmd(key, objSet)
		}
		set, ok := robj.Value().(*set.Set)
		if !ok {
			return nil
		}
		return setToCmd(key, set)
	case object.TypeHash:
		hash, ok := robj.Value().(*object.Hash)
		if !ok {
			return nil
		}
		return objectHashToCmd(key, hash)
	case object.TypeZSet:
		zset, ok := robj.Value().(*object.ZSet)
		if !ok {
			return nil
		}
		return objectZSetToCmd(key, zset)
	}
	return nil
}

var setCmd = []byte("SET")

func stringToCmd(key string, bytes []byte) *protocol.MultiBulkReply {
	args := make([][]byte, 3)
	args[0] = setCmd
	args[1] = []byte(key)
	args[2] = bytes
	return protocol.MakeMultiBulkReply(args)
}

var rPushAllCmd = []byte("RPUSH")

func objectListToCmd(key string, list *object.List) *protocol.MultiBulkReply {
	args := make([][]byte, 2+list.Len())
	args[0] = rPushAllCmd
	args[1] = []byte(key)
	list.ForEach(func(i int, val interface{}) bool {
		bytes, _ := val.([]byte)
		args[2+i] = bytes
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}

func listToCmd(key string, list List.List) *protocol.MultiBulkReply {
	args := make([][]byte, 2+list.Len())
	args[0] = rPushAllCmd
	args[1] = []byte(key)
	list.ForEach(func(i int, val interface{}) bool {
		bytes, _ := val.([]byte)
		args[2+i] = bytes
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}

var sAddCmd = []byte("SADD")

func setToCmd(key string, set *set.Set) *protocol.MultiBulkReply {
	args := make([][]byte, 2+set.Len())
	args[0] = sAddCmd
	args[1] = []byte(key)
	i := 0
	set.ForEach(func(val string) bool {
		args[2+i] = []byte(val)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}

var hMSetCmd = []byte("HMSET")

func hashToCmd(key string, hash dict.Dict) *protocol.MultiBulkReply {
	args := make([][]byte, 2+hash.Len()*2)
	args[0] = hMSetCmd
	args[1] = []byte(key)
	i := 0
	hash.ForEach(func(field string, val interface{}) bool {
		bytes, _ := val.([]byte)
		args[2+i*2] = []byte(field)
		args[3+i*2] = bytes
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}

// objectHashToCmd serializes an object.Hash without converting its encoding.
// Uses Hash.ForEach which works on both listpack and hashtable encodings.
func objectHashToCmd(key string, hash *object.Hash) *protocol.MultiBulkReply {
	args := make([][]byte, 2+hash.Len()*2)
	args[0] = hMSetCmd
	args[1] = []byte(key)
	i := 0
	hash.ForEach(func(field string, val interface{}) bool {
		switch v := val.(type) {
		case []byte:
			args[2+i*2] = []byte(field)
			args[3+i*2] = v
		case string:
			args[2+i*2] = []byte(field)
			args[3+i*2] = []byte(v)
		default:
			args[2+i*2] = []byte(field)
			args[3+i*2] = []byte("")
		}
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}

var zAddCmd = []byte("ZADD")

func objectZSetToCmd(key string, zset *object.ZSet) *protocol.MultiBulkReply {
	args := make([][]byte, 2+zset.Len()*2)
	args[0] = zAddCmd
	args[1] = []byte(key)
	i := 0
	zset.ForEach(func(member string, score float64) bool {
		value := strconv.FormatFloat(score, 'f', -1, 64)
		args[2+i*2] = []byte(value)
		args[3+i*2] = []byte(member)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}

func zSetToCmd(key string, zset *SortedSet.SortedSet) *protocol.MultiBulkReply {
	args := make([][]byte, 2+zset.Len()*2)
	args[0] = zAddCmd
	args[1] = []byte(key)
	i := 0
	zset.ForEachByRank(int64(0), int64(zset.Len()), true, func(element *SortedSet.Element) bool {
		value := strconv.FormatFloat(element.Score, 'f', -1, 64)
		args[2+i*2] = []byte(value)
		args[3+i*2] = []byte(element.Member)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}

var pExpireAtBytes = []byte("PEXPIREAT")

// MakeExpireCmd generates command line to set expiration for the given key
func MakeExpireCmd(key string, expireAt time.Time) *protocol.MultiBulkReply {
	args := make([][]byte, 3)
	args[0] = pExpireAtBytes
	args[1] = []byte(key)
	args[2] = []byte(strconv.FormatInt(expireAt.UnixNano()/1e6, 10))
	return protocol.MakeMultiBulkReply(args)
}

func objectSetToCmd(key string, set *object.Set) *protocol.MultiBulkReply {
	args := make([][]byte, 2+set.Len())
	args[0] = sAddCmd
	args[1] = []byte(key)
	i := 0
	set.ForEach(func(val string) bool {
		args[2+i] = []byte(val)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(args)
}
