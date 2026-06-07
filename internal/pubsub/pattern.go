package pubsub

import (
	"strconv"

	"github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/lib/wildcard"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

var (
	_psubscribe   = "psubscribe"
	_punsubscribe = "punsubscribe"
	pmessageBytes = []byte("pmessage")

	punsubscribeNothing = []byte("*3\r\n$11\r\npunsubscribe\r\n$-1\n:0\r\n")
)

func makePMsg(t string, pattern string, code int64) []byte {
	return []byte("*3\r\n$" + strconv.FormatInt(int64(len(t)), 10) + protocol.CRLF + t + protocol.CRLF +
		"$" + strconv.FormatInt(int64(len(pattern)), 10) + protocol.CRLF + pattern + protocol.CRLF +
		":" + strconv.FormatInt(code, 10) + protocol.CRLF)
}

/*
 * invoker should lock pattern
 * return: is new subscribed
 */
func psubscribe0(hub *Hub, pattern string, client redis.Connection) bool {
	client.PSubscribe(pattern)

	// add into hub.patterns
	raw, ok := hub.patterns.Get(pattern)
	var subscribers *list.LinkedList
	if ok {
		subscribers, _ = raw.(*list.LinkedList)
	} else {
		subscribers = list.Make()
		hub.patterns.Put(pattern, subscribers)
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
 * invoker should lock pattern
 * return: is actually un-subscribe
 */
func punsubscribe0(hub *Hub, pattern string, client redis.Connection) bool {
	client.PUnSubscribe(pattern)

	// remove from hub.patterns
	raw, ok := hub.patterns.Get(pattern)
	if ok {
		subscribers, _ := raw.(*list.LinkedList)
		subscribers.RemoveAllByVal(func(a interface{}) bool {
			return utils.Equals(a, client)
		})

		if subscribers.Len() == 0 {
			hub.patterns.Remove(pattern)
		}
		return true
	}
	return false
}

// PSubscribe puts the given connection into the given pattern subscription
func PSubscribe(hub *Hub, c redis.Connection, args [][]byte) redis.Reply {
	patterns := make([]string, len(args))
	for i, b := range args {
		patterns[i] = string(b)
	}

	hub.patternLock.Locks(patterns...)
	defer hub.patternLock.UnLocks(patterns...)

	for _, pattern := range patterns {
		if psubscribe0(hub, pattern, c) {
			_, _ = c.Write(makePMsg(_psubscribe, pattern, int64(c.SubsCount()+c.PatternCount())))
		}
	}
	return &protocol.NoReply{}
}

// PUnsubscribe removes the given connection from the given pattern subscription
func PUnsubscribe(hub *Hub, c redis.Connection, args [][]byte) redis.Reply {
	var patterns []string
	if len(args) > 0 {
		patterns = make([]string, len(args))
		for i, b := range args {
			patterns[i] = string(b)
		}
	} else {
		patterns = c.GetPatterns()
	}

	hub.patternLock.Locks(patterns...)
	defer hub.patternLock.UnLocks(patterns...)

	if len(patterns) == 0 {
		_, _ = c.Write(punsubscribeNothing)
		return &protocol.NoReply{}
	}

	for _, pattern := range patterns {
		if punsubscribe0(hub, pattern, c) {
			_, _ = c.Write(makePMsg(_punsubscribe, pattern, int64(c.SubsCount()+c.PatternCount())))
		}
	}
	return &protocol.NoReply{}
}

// PUnsubscribeAll removes the given connection from all pattern subscriptions
func PUnsubscribeAll(hub *Hub, c redis.Connection) {
	patterns := c.GetPatterns()

	hub.patternLock.Locks(patterns...)
	defer hub.patternLock.UnLocks(patterns...)

	for _, pattern := range patterns {
		punsubscribe0(hub, pattern, c)
	}
}

// publishToPatterns delivers a message to all pattern subscribers whose
// pattern matches the channel. Returns the count of clients that received it.
func publishToPatterns(hub *Hub, channel string, message []byte) int {
	var count int
	hub.patterns.ForEach(func(pattern string, raw interface{}) bool {
		p, err := wildcard.CompilePattern(pattern)
		if err != nil {
			return true // skip invalid patterns
		}
		if !p.IsMatch(channel) {
			return true
		}
		subscribers, _ := raw.(*list.LinkedList)
		subscribers.ForEach(func(i int, c interface{}) bool {
			client, _ := c.(redis.Connection)
			replyArgs := make([][]byte, 4)
			replyArgs[0] = pmessageBytes
			replyArgs[1] = []byte(pattern)
			replyArgs[2] = []byte(channel)
			replyArgs[3] = message
			_, _ = client.Write(protocol.MakeMultiBulkReply(replyArgs).ToBytes())
			return true
		})
		count += subscribers.Len()
		return true
	})
	return count
}

// PubSub handles the PUBSUB introspection command
func PubSub(hub *Hub, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("wrong number of arguments for 'pubsub' command")
	}

	subCmd := string(args[0])
	switch subCmd {
	case "channels":
		return pubSubChannels(hub, args[1:])
	case "numsub":
		return pubSubNumSub(hub, args[1:])
	case "numpat":
		return pubSubNumPat(hub)
	default:
		return protocol.MakeErrReply("unknown subcommand '" + subCmd + "'. Try CHANNELS, NUMSUB, NUMPAT.")
	}
}

// pubSubChannels implements PUBSUB CHANNELS [pattern]
func pubSubChannels(hub *Hub, args [][]byte) redis.Reply {
	var pattern *wildcard.Pattern
	if len(args) > 0 {
		p, err := wildcard.CompilePattern(string(args[0]))
		if err != nil {
			return protocol.MakeErrReply("invalid pattern")
		}
		pattern = p
	}

	var result [][]byte
	hub.subs.ForEach(func(channel string, _ interface{}) bool {
		if pattern == nil || pattern.IsMatch(channel) {
			result = append(result, []byte(channel))
		}
		return true
	})
	return protocol.MakeMultiBulkReply(result)
}

// pubSubNumSub implements PUBSUB NUMSUB [channel ...]
func pubSubNumSub(hub *Hub, args [][]byte) redis.Reply {
	result := make([][]byte, 0, len(args)*2)
	for _, b := range args {
		channel := string(b)
		result = append(result, []byte(channel))
		raw, ok := hub.subs.Get(channel)
		if ok {
			subscribers, _ := raw.(*list.LinkedList)
			result = append(result, []byte(strconv.FormatInt(int64(subscribers.Len()), 10)))
		} else {
			result = append(result, []byte("0"))
		}
	}
	return protocol.MakeMultiBulkReply(result)
}

// pubSubNumPat implements PUBSUB NUMPAT
func pubSubNumPat(hub *Hub) redis.Reply {
	return protocol.MakeIntReply(int64(hub.patterns.Len()))
}
