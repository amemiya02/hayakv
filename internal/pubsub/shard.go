package pubsub

import (
	"strconv"

	"github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

var (
	_ssubscribe   = "ssubscribe"
	_sunsubscribe = "sunsubscribe"
	smessageBytes = []byte("smessage")

	sunsubscribeNothing = []byte("*3\r\n$12\r\nsunsubscribe\r\n$-1\n:0\r\n")
)

func makeSMsg(t string, channel string, code int64) []byte {
	return []byte("*3\r\n$" + strconv.FormatInt(int64(len(t)), 10) + protocol.CRLF + t + protocol.CRLF +
		"$" + strconv.FormatInt(int64(len(channel)), 10) + protocol.CRLF + channel + protocol.CRLF +
		":" + strconv.FormatInt(code, 10) + protocol.CRLF)
}

/*
 * invoker should lock channel
 * return: is new subscribed
 */
func ssubscribe0(hub *Hub, channel string, client redis.Connection) bool {
	client.Subscribe(channel)

	// add into hub.shardSubs
	raw, ok := hub.shardSubs.Get(channel)
	var subscribers *list.LinkedList
	if ok {
		subscribers, _ = raw.(*list.LinkedList)
	} else {
		subscribers = list.Make()
		hub.shardSubs.Put(channel, subscribers)
	}
	if subscribers.Contains(func(a interface{}) bool {
		return a == client
	}) {
		return false
	}
	subscribers.Add(client)
	return true
}

/*
 * invoker should lock channel
 * return: is actually un-subscribe
 */
func sunsubscribe0(hub *Hub, channel string, client redis.Connection) bool {
	client.UnSubscribe(channel)

	// remove from hub.shardSubs
	raw, ok := hub.shardSubs.Get(channel)
	if ok {
		subscribers, _ := raw.(*list.LinkedList)
		subscribers.RemoveAllByVal(func(a interface{}) bool {
			return utils.Equals(a, client)
		})

		if subscribers.Len() == 0 {
			hub.shardSubs.Remove(channel)
		}
		return true
	}
	return false
}

// SSubscribe subscribes the connection to the given shard channels.
// For each channel, a `ssubscribe` confirmation is pushed.
func SSubscribe(hub *Hub, c redis.Connection, args [][]byte) redis.Reply {
	channels := make([]string, len(args))
	for i, b := range args {
		channels[i] = string(b)
	}

	hub.shardLocker.Locks(channels...)
	defer hub.shardLocker.UnLocks(channels...)

	for _, channel := range channels {
		if ssubscribe0(hub, channel, c) {
			_, _ = c.Write(makeSMsg(_ssubscribe, channel, int64(c.SubsCount())))
		}
	}
	return &protocol.NoReply{}
}

// SUnsubscribeAll removes the given connection from all shard channel subscriptions
func SUnsubscribeAll(hub *Hub, c redis.Connection) {
	channels := getShardChannels(hub, c)

	hub.shardLocker.Locks(channels...)
	defer hub.shardLocker.UnLocks(channels...)

	for _, channel := range channels {
		sunsubscribe0(hub, channel, c)
	}
}

// SUnsubscribe removes the given connection from the given shard channels.
// For each channel, a `sunsubscribe` confirmation is pushed.
func SUnsubscribe(hub *Hub, c redis.Connection, args [][]byte) redis.Reply {
	var channels []string
	if len(args) > 0 {
		channels = make([]string, len(args))
		for i, b := range args {
			channels[i] = string(b)
		}
	} else {
		channels = getShardChannels(hub, c)
	}

	hub.shardLocker.Locks(channels...)
	defer hub.shardLocker.UnLocks(channels...)

	if len(channels) == 0 {
		_, _ = c.Write(sunsubscribeNothing)
		return &protocol.NoReply{}
	}

	for _, channel := range channels {
		if sunsubscribe0(hub, channel, c) {
			_, _ = c.Write(makeSMsg(_sunsubscribe, channel, int64(c.SubsCount())))
		}
	}
	return &protocol.NoReply{}
}

// SPublish publishes a message to a shard channel. Returns the number of
// clients that received the message.
func SPublish(hub *Hub, args [][]byte) redis.Reply {
	if len(args) != 2 {
		return &protocol.ArgNumErrReply{Cmd: "spublish"}
	}
	channel := string(args[0])
	message := args[1]

	hub.shardLocker.Lock(channel)
	defer hub.shardLocker.UnLock(channel)

	var count int
	raw, ok := hub.shardSubs.Get(channel)
	if ok {
		subscribers, _ := raw.(*list.LinkedList)
		subscribers.ForEach(func(i int, c interface{}) bool {
			client, _ := c.(redis.Connection)
			replyArgs := make([][]byte, 3)
			replyArgs[0] = smessageBytes
			replyArgs[1] = []byte(channel)
			replyArgs[2] = message
			_, _ = client.Write(protocol.MakeMultiBulkReply(replyArgs).ToBytes())
			return true
		})
		count = subscribers.Len()
	}

	return protocol.MakeIntReply(int64(count))
}

// getShardChannels returns all shard channels the connection is subscribed to.
func getShardChannels(hub *Hub, c redis.Connection) []string {
	var channels []string
	hub.shardSubs.ForEach(func(channel string, raw interface{}) bool {
		subscribers, _ := raw.(*list.LinkedList)
		if subscribers.Contains(func(a interface{}) bool {
			return a == c
		}) {
			channels = append(channels, channel)
		}
		return true
	})
	return channels
}
