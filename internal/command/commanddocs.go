package database

import (
	"strings"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

type cmdDoc struct {
	summary string
	since   string
	group   string
	arity   int
}

var cmdDocs = map[string]cmdDoc{
	"get":        {summary: "Get the value of a key", since: "1.0.0", group: "string"},
	"set":        {summary: "Set the string value of a key", since: "1.0.0", group: "string"},
	"del":        {summary: "Delete one or more keys", since: "1.0.0", group: "generic"},
	"expire":     {summary: "Set a key's time to live in seconds", since: "1.0.0", group: "generic"},
	"ttl":        {summary: "Get the time to live for a key", since: "1.0.0", group: "generic"},
	"lpush":      {summary: "Prepend one or more elements to a list", since: "1.0.0", group: "list"},
	"rpush":      {summary: "Append one or more elements to a list", since: "1.0.0", group: "list"},
	"lpop":       {summary: "Remove and get the first element in a list", since: "1.0.0", group: "list"},
	"rpop":       {summary: "Remove and get the last element in a list", since: "1.0.0", group: "list"},
	"llen":       {summary: "Get the length of a list", since: "1.0.0", group: "list"},
	"hset":       {summary: "Set the string value of a hash field", since: "2.0.0", group: "hash"},
	"hget":       {summary: "Get the value of a hash field", since: "2.0.0", group: "hash"},
	"hdel":       {summary: "Delete one or more hash fields", since: "2.0.0", group: "hash"},
	"hgetall":    {summary: "Get all the fields and values in a hash", since: "2.0.0", group: "hash"},
	"sadd":       {summary: "Add one or more members to a set", since: "1.0.0", group: "set"},
	"srem":       {summary: "Remove one or more members from a set", since: "1.0.0", group: "set"},
	"smembers":   {summary: "Get all the members in a set", since: "1.0.0", group: "set"},
	"zadd":       {summary: "Add one or more members to a sorted set", since: "1.2.0", group: "sorted-set"},
	"zrange":     {summary: "Return a range of members in a sorted set", since: "1.2.0", group: "sorted-set"},
	"zrem":       {summary: "Remove one or more members from a sorted set", since: "1.2.0", group: "sorted-set"},
	"ping":       {summary: "Ping the server", since: "1.0.0", group: "connection"},
	"echo":       {summary: "Echo the given string", since: "1.0.0", group: "connection"},
	"select":     {summary: "Change the selected database", since: "1.0.0", group: "connection"},
	"auth":       {summary: "Authenticate to the server", since: "1.0.0", group: "connection"},
	"info":       {summary: "Get information and statistics about the server", since: "1.0.0", group: "server"},
	"config":     {summary: "Get or set configuration parameters", since: "2.0.0", group: "server"},
	"flushdb":    {summary: "Remove all keys from the current database", since: "1.0.0", group: "server"},
	"flushall":   {summary: "Remove all keys from all databases", since: "1.0.0", group: "server"},
	"subscribe":  {summary: "Listen for messages published to channels", since: "2.0.0", group: "pubsub"},
	"publish":    {summary: "Post a message to a channel", since: "2.0.0", group: "pubsub"},
	"multi":      {summary: "Mark the start of a transaction block", since: "1.2.0", group: "transactions"},
	"exec":       {summary: "Execute all commands issued after MULTI", since: "1.2.0", group: "transactions"},
	"watch":      {summary: "Watch the keys to determine execution of MULTI", since: "2.2.0", group: "transactions"},
	"discard":    {summary: "Discard all commands issued after MULTI", since: "2.0.0", group: "transactions"},
	"eval":       {summary: "Execute a Lua script server side", since: "2.6.0", group: "scripting"},
	"client":     {summary: "A container for client connection commands", since: "2.4.0", group: "connection"},
	"monitor":    {summary: "Listen for all requests received by the server", since: "1.0.0", group: "server"},
	"latency":    {summary: "A container for latency diagnostics commands", since: "2.8.12", group: "server"},
	"memory":     {summary: "A container for memory diagnostics commands", since: "4.0.0", group: "server"},
	"slowlog":    {summary: "Manage the slow queries log", since: "2.2.12", group: "server"},
	"command":    {summary: "A container for Redis commands", since: "2.8.13", group: "server"},
	"lpos":       {summary: "Return the index of matching elements on list", since: "6.0.6", group: "list"},
	"smismember": {summary: "Returns the membership associated with given members", since: "6.2.0", group: "set"},
	"sintercard": {summary: "Intersect multiple sets and return the cardinality", since: "7.0.0", group: "set"},
	"lmpop":      {summary: "Pop elements from a list", since: "7.0.0", group: "list"},
	"zmpop":      {summary: "Pop members from a sorted set", since: "7.0.0", group: "sorted-set"},
	"copy":       {summary: "Copy a key", since: "6.2.0", group: "generic"},
	"getdel":     {summary: "Get the value of a key and delete the key", since: "6.2.0", group: "string"},
	"getex":      {summary: "Get the value of a key and optionally set its expiration", since: "6.2.0", group: "string"},
}

func getCommandDocs(args [][]byte) redis.Reply {
	if len(args) == 0 {
		// Return all docs
		result := make([]redis.Reply, 0, len(cmdDocs)*2)
		for name, doc := range cmdDocs {
			result = append(result, protocol.MakeBulkReply([]byte(name)))
			result = append(result, docToReply(doc))
		}
		return protocol.MakeMultiRawReply(result)
	}

	result := make([]redis.Reply, 0, len(args)*2)
	for _, arg := range args {
		name := strings.ToLower(string(arg))
		doc, ok := cmdDocs[name]
		if !ok {
			continue
		}
		result = append(result, protocol.MakeBulkReply([]byte(name)))
		result = append(result, docToReply(doc))
	}
	return protocol.MakeMultiRawReply(result)
}

func docToReply(doc cmdDoc) redis.Reply {
	return protocol.MakeMultiRawReply([]redis.Reply{
		protocol.MakeBulkReply([]byte("summary")),
		protocol.MakeBulkReply([]byte(doc.summary)),
		protocol.MakeBulkReply([]byte("since")),
		protocol.MakeBulkReply([]byte(doc.since)),
		protocol.MakeBulkReply([]byte("group")),
		protocol.MakeBulkReply([]byte(doc.group)),
		protocol.MakeBulkReply([]byte("arity")),
		protocol.MakeIntReply(int64(doc.arity)),
	})
}
