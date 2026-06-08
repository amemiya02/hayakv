package pubsub

import (
	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/datastruct/lock"
)

// Hub stores all subscribe relations
type Hub struct {
	// channel -> list(*Client)
	subs dict.Dict
	// lock channel
	subsLocker *lock.Locks
	// pattern -> list(*Client)
	patterns dict.Dict
	// lock pattern
	patternLock *lock.Locks
	// shard channel -> list(redis.Connection)  (sharded pub/sub)
	shardSubs dict.Dict
	// lock shard channel
	shardLocker *lock.Locks
}

// MakeHub creates new hub
func MakeHub() *Hub {
	return &Hub{
		subs:        dict.MakeConcurrent(4),
		subsLocker:  lock.Make(16),
		patterns:    dict.MakeConcurrent(4),
		patternLock: lock.Make(16),
		shardSubs:   dict.MakeConcurrent(4),
		shardLocker: lock.Make(16),
	}
}
