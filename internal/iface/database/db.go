package database

import (
	"time"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/hdt3213/rdb/core"
)

// CmdLine is alias for [][]byte, represents a command line
type CmdLine = [][]byte

// DB is the interface for redis style storage engine
type DB interface {
	Exec(client redis.Connection, cmdLine [][]byte) redis.Reply
	AfterClientClose(c redis.Connection)
	Close()
}

// KeyEventCallback will be called back on key event, such as key inserted or deleted
// may be called concurrently
type KeyEventCallback func(dbIndex int, key string, entity *DataEntity)

// DBEngine is the embedding storage engine exposing more methods for complex application
type DBEngine interface {
	DB
	LoadRDB(dec *core.Decoder) error
	ExecWithLock(conn redis.Connection, cmdLine [][]byte) redis.Reply
	ExecMulti(conn redis.Connection, watching map[string]uint32, cmdLines []CmdLine) redis.Reply
	GetUndoLogs(dbIndex int, cmdLine [][]byte) []CmdLine
	ForEach(dbIndex int, cb func(key string, data *DataEntity, expiration *time.Time) bool)
	RWLocks(dbIndex int, writeKeys []string, readKeys []string)
	RWUnLocks(dbIndex int, writeKeys []string, readKeys []string)
	GetDBSize(dbIndex int) (int, int)
	GetEntity(dbIndex int, key string) (*DataEntity, bool)
	GetExpiration(dbIndex int, key string) *time.Time
	SetKeyInsertedCallback(cb KeyEventCallback)
	SetKeyDeletedCallback(cb KeyEventCallback)
}

// DataEntity stores data bound to a key, including a string, list, hash, set and so on
type DataEntity struct {
	Data interface{}
}

// TypeName returns the object type name (legacy placeholder for M0)
func (e *DataEntity) TypeName() string {
	return "legacy"
}

// EncodingName returns the encoding name (legacy placeholder for M0)
func (e *DataEntity) EncodingName() string {
	return "go-native"
}

// Value returns the underlying data, or nil if the receiver is nil
func (e *DataEntity) Value() any {
	if e == nil {
		return nil
	}
	return e.Data
}
