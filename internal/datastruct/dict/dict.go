package dict

// Consumer is used to traversal dict, if it returns false the traversal will be break
type Consumer func(key string, val interface{}) bool

// Dict is interface of a key-value data structure
type Dict interface {
	Get(key string) (val interface{}, exists bool)
	Len() int
	Put(key string, val interface{}) (result int)
	PutIfAbsent(key string, val interface{}) (result int)
	PutIfExists(key string, val interface{}) (result int)
	Remove(key string) (val interface{}, result int)
	ForEach(consumer Consumer)
	Keys() []string
	RandomKeys(limit int) []string
	RandomDistinctKeys(limit int) []string
	Clear()
	DictScan(cursor int, count int, pattern string) ([][]byte, int)
	// Locking methods: caller already holds the lock (via RWLocks).
	// ConcurrentDict uses per-shard locks; RedisDict delegates to the
	// underlying Get/Put/Remove (no-op locks, outer lock provides safety).
	GetWithLock(key string) (val interface{}, exists bool)
	PutWithLock(key string, val interface{}) (result int)
	PutIfAbsentWithLock(key string, val interface{}) (result int)
	PutIfExistsWithLock(key string, val interface{}) (result int)
	RemoveWithLock(key string) (val interface{}, result int)
	RWLocks(writeKeys []string, readKeys []string)
	RWUnLocks(writeKeys []string, readKeys []string)
}
