package database

import (
	"strings"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// cmdArg describes a single argument in a Redis command's signature.
// It mirrors the argument specification returned by COMMAND DOCS, which
// redis-cli uses for inline hints and tab-completion.
type cmdArg struct {
	name          string   // argument name identifier (e.g. "key", "value", "seconds")
	typ           string   // "key", "string", "integer", "double", "pattern", "unix-time", "pure-token", "oneof", "block"
	displayText   string   // display text for the hint; omit if same as name
	token         string   // preceding token for token+value args (e.g. "EX", "NX")
	optional      bool     // whether the argument is optional
	multiple      bool     // whether the argument can repeat
	keySpecIdx    int      // key_spec_index (0-based); only emitted when hasKeySpecIdx is true
	hasKeySpecIdx bool     // true when keySpecIdx was explicitly set
	since         string   // version that added this argument (omit if original)
	flags         []string // explicit flags to emit (e.g. "optional", "multiple")
	subArgs       []cmdArg // nested arguments for oneof/block types
}

type cmdDoc struct {
	summary    string
	since      string
	group      string
	complexity string
	arguments  []cmdArg
}

// ---------------------------------------------------------------------------
// Argument spec helpers — keep the cmdDocs table below concise
// ---------------------------------------------------------------------------

// ak returns a simple key argument with key_spec_index 0.
func ak(display string) cmdArg {
	return cmdArg{name: display, typ: "key", displayText: display, keySpecIdx: 0, hasKeySpecIdx: true}
}

// akN returns a key argument with a specific key_spec_index.
func akN(display string, idx int) cmdArg {
	return cmdArg{name: display, typ: "key", displayText: display, keySpecIdx: idx, hasKeySpecIdx: true}
}

// as returns a string argument.
func as(name string) cmdArg {
	return cmdArg{name: name, typ: "string", displayText: name}
}

// ai returns an integer argument.
func ai(name string) cmdArg {
	return cmdArg{name: name, typ: "integer", displayText: name}
}

// ad returns a double argument.
func ad(name string) cmdArg {
	return cmdArg{name: name, typ: "double", displayText: name}
}

// at returns a pure-token argument (e.g. NX, XX).
func at(name, token string) cmdArg {
	return cmdArg{name: name, typ: "pure-token", displayText: name, token: token}
}

// opt marks an argument as optional.
func opt(a cmdArg) cmdArg {
	a.optional = true
	return a
}

// mul marks an argument as multiple (repeatable).
func mul(a cmdArg) cmdArg {
	a.multiple = true
	return a
}

// sinceArg sets the since version on an argument.
func sinceArg(a cmdArg, v string) cmdArg {
	a.since = v
	return a
}

// tok sets the token field on an argument (for integer+token patterns like EX <seconds>).
func tok(a cmdArg, token string) cmdArg {
	a.token = token
	return a
}

// oneof returns an oneof argument with the given sub-arguments.
func oneof(name string, subs ...cmdArg) cmdArg {
	return cmdArg{name: name, typ: "oneof", subArgs: subs}
}

// block returns a block argument with the given sub-arguments.
func block(name string, subs ...cmdArg) cmdArg {
	return cmdArg{name: name, typ: "block", subArgs: subs}
}

// ---------------------------------------------------------------------------
// Command documentation table
// ---------------------------------------------------------------------------

var cmdDocs = map[string]cmdDoc{
	// --- string ---
	"get": {
		summary: "Get the value of a key", since: "1.0.0", group: "string",
		arguments: []cmdArg{ak("key")},
	},
	"set": {
		summary: "Set the string value of a key", since: "1.0.0", group: "string",
		arguments: []cmdArg{
			ak("key"),
			as("value"),
			opt(oneof("condition",
				at("nx", "NX"),
				at("xx", "XX"),
			)),
			opt(at("get", "GET")),
			opt(oneof("expiration",
				tok(ai("seconds"), "EX"),
				tok(ai("milliseconds"), "PX"),
				tok(ai("unix-time-seconds"), "EXAT"),
				tok(ai("unix-time-milliseconds"), "PXAT"),
				at("keepttl", "KEEPTTL"),
			)),
		},
	},
	"del": {
		summary: "Delete one or more keys", since: "1.0.0", group: "generic",
		arguments: []cmdArg{mul(ak("key"))},
	},
	"copy": {
		summary: "Copy a key", since: "6.2.0", group: "generic",
		arguments: []cmdArg{
			ak("source"),
			akN("destination", 1),
			opt(tok(ai("destination-db"), "DB")),
			opt(at("replace", "REPLACE")),
		},
	},
	"getdel": {
		summary: "Get the value of a key and delete the key", since: "6.2.0", group: "string",
		arguments: []cmdArg{ak("key")},
	},
	"getex": {
		summary: "Get the value of a key and optionally set its expiration", since: "6.2.0", group: "string",
		arguments: []cmdArg{
			ak("key"),
			opt(oneof("expiration",
				tok(ai("seconds"), "EX"),
				tok(ai("milliseconds"), "PX"),
				tok(ai("unix-time-seconds"), "EXAT"),
				tok(ai("unix-time-milliseconds"), "PXAT"),
				at("persist", "PERSIST"),
			)),
		},
	},
	// --- generic ---
	"expire": {
		summary: "Set a key's time to live in seconds", since: "1.0.0", group: "generic",
		arguments: []cmdArg{
			ak("key"),
			ai("seconds"),
			opt(oneof("condition",
				at("nx", "NX"),
				at("xx", "XX"),
				at("gt", "GT"),
				at("lt", "LT"),
			)),
		},
	},
	"ttl": {
		summary: "Get the time to live for a key", since: "1.0.0", group: "generic",
		arguments: []cmdArg{ak("key")},
	},
	// --- list ---
	"lpush": {
		summary: "Prepend one or more elements to a list", since: "1.0.0", group: "list",
		arguments: []cmdArg{ak("key"), mul(as("element"))},
	},
	"rpush": {
		summary: "Append one or more elements to a list", since: "1.0.0", group: "list",
		arguments: []cmdArg{ak("key"), mul(as("element"))},
	},
	"lpop": {
		summary: "Remove and get the first element in a list", since: "1.0.0", group: "list",
		arguments: []cmdArg{ak("key"), opt(ai("count"))},
	},
	"rpop": {
		summary: "Remove and get the last element in a list", since: "1.0.0", group: "list",
		arguments: []cmdArg{ak("key"), opt(ai("count"))},
	},
	"llen": {
		summary: "Get the length of a list", since: "1.0.0", group: "list",
		arguments: []cmdArg{ak("key")},
	},
	"lpos": {
		summary: "Return the index of matching elements on list", since: "6.0.6", group: "list",
		arguments: []cmdArg{
			ak("key"),
			as("element"),
			opt(tok(ai("rank"), "RANK")),
			opt(tok(ai("num-matches"), "COUNT")),
			opt(tok(ai("len"), "MAXLEN")),
		},
	},
	"lmpop": {
		summary: "Pop elements from a list", since: "7.0.0", group: "list",
		arguments: []cmdArg{
			ai("numkeys"),
			mul(ak("key")),
			oneof("where", at("left", "LEFT"), at("right", "RIGHT")),
			opt(tok(ai("count"), "COUNT")),
		},
	},
	// --- hash ---
	"hset": {
		summary: "Set the string value of a hash field", since: "2.0.0", group: "hash",
		arguments: []cmdArg{
			ak("key"),
			mul(block("data", as("field"), as("value"))),
		},
	},
	"hget": {
		summary: "Get the value of a hash field", since: "2.0.0", group: "hash",
		arguments: []cmdArg{ak("key"), as("field")},
	},
	"hdel": {
		summary: "Delete one or more hash fields", since: "2.0.0", group: "hash",
		arguments: []cmdArg{ak("key"), mul(as("field"))},
	},
	"hgetall": {
		summary: "Get all the fields and values in a hash", since: "2.0.0", group: "hash",
		arguments: []cmdArg{ak("key")},
	},
	// --- set ---
	"sadd": {
		summary: "Add one or more members to a set", since: "1.0.0", group: "set",
		arguments: []cmdArg{ak("key"), mul(as("member"))},
	},
	"srem": {
		summary: "Remove one or more members from a set", since: "1.0.0", group: "set",
		arguments: []cmdArg{ak("key"), mul(as("member"))},
	},
	"smembers": {
		summary: "Get all the members in a set", since: "1.0.0", group: "set",
		arguments: []cmdArg{ak("key")},
	},
	"smismember": {
		summary: "Returns the membership associated with given members", since: "6.2.0", group: "set",
		arguments: []cmdArg{ak("key"), mul(as("member"))},
	},
	"sintercard": {
		summary: "Intersect multiple sets and return the cardinality", since: "7.0.0", group: "set",
		arguments: []cmdArg{
			ai("numkeys"),
			mul(ak("key")),
			opt(tok(ai("limit"), "LIMIT")),
		},
	},
	// --- sorted-set ---
	"zadd": {
		summary: "Add one or more members to a sorted set", since: "1.2.0", group: "sorted-set",
		arguments: []cmdArg{
			ak("key"),
			opt(oneof("condition", at("nx", "NX"), at("xx", "XX"))),
			opt(oneof("comparison", at("gt", "GT"), at("lt", "LT"))),
			opt(at("change", "CH")),
			opt(at("increment", "INCR")),
			mul(block("data", ad("score"), as("member"))),
		},
	},
	"zrange": {
		summary: "Return a range of members in a sorted set", since: "1.2.0", group: "sorted-set",
		arguments: []cmdArg{
			ak("key"),
			as("start"),
			as("stop"),
			opt(oneof("sortby", at("byscore", "BYSCORE"), at("bylex", "BYLEX"))),
			opt(at("rev", "REV")),
			opt(block("limit",
				as("offset"),
				as("count"),
			)),
			opt(at("withscores", "WITHSCORES")),
		},
	},
	"zrem": {
		summary: "Remove one or more members from a sorted set", since: "1.2.0", group: "sorted-set",
		arguments: []cmdArg{ak("key"), mul(as("member"))},
	},
	"zmpop": {
		summary: "Pop members from a sorted set", since: "7.0.0", group: "sorted-set",
		arguments: []cmdArg{
			ai("numkeys"),
			mul(ak("key")),
			oneof("where", at("min", "MIN"), at("max", "MAX")),
			opt(tok(ai("count"), "COUNT")),
		},
	},
	// --- connection ---
	"ping": {
		summary: "Ping the server", since: "1.0.0", group: "connection",
		arguments: []cmdArg{opt(as("message"))},
	},
	"echo": {
		summary: "Echo the given string", since: "1.0.0", group: "connection",
		arguments: []cmdArg{as("message")},
	},
	"select": {
		summary: "Change the selected database", since: "1.0.0", group: "connection",
		arguments: []cmdArg{ai("index")},
	},
	"auth": {
		summary: "Authenticate to the server", since: "1.0.0", group: "connection",
		arguments: []cmdArg{opt(as("username")), as("password")},
	},
	// --- server ---
	"info": {
		summary: "Get information and statistics about the server", since: "1.0.0", group: "server",
		arguments: []cmdArg{opt(as("section"))},
	},
	"config": {
		summary: "Get or set configuration parameters", since: "2.0.0", group: "server",
	},
	"flushdb": {
		summary: "Remove all keys from the current database", since: "1.0.0", group: "server",
		arguments: []cmdArg{
			opt(oneof("flush-type", at("async", "ASYNC"), at("sync", "SYNC"))),
		},
	},
	"flushall": {
		summary: "Remove all keys from all databases", since: "1.0.0", group: "server",
		arguments: []cmdArg{
			opt(oneof("flush-type", at("async", "ASYNC"), at("sync", "SYNC"))),
		},
	},
	// --- pubsub ---
	"subscribe": {
		summary: "Listen for messages published to channels", since: "2.0.0", group: "pubsub",
		arguments: []cmdArg{mul(as("channel"))},
	},
	"publish": {
		summary: "Post a message to a channel", since: "2.0.0", group: "pubsub",
		arguments: []cmdArg{as("channel"), as("message")},
	},
	// --- transactions ---
	"multi": {
		summary: "Mark the start of a transaction block", since: "1.2.0", group: "transactions",
	},
	"exec": {
		summary: "Execute all commands issued after MULTI", since: "1.2.0", group: "transactions",
	},
	"watch": {
		summary: "Watch the keys to determine execution of MULTI", since: "2.2.0", group: "transactions",
		arguments: []cmdArg{mul(ak("key"))},
	},
	"discard": {
		summary: "Discard all commands issued after MULTI", since: "2.0.0", group: "transactions",
	},
	// --- scripting ---
	"eval": {
		summary: "Execute a Lua script server side", since: "2.6.0", group: "scripting",
		arguments: []cmdArg{
			as("script"),
			ai("numkeys"),
			mul(opt(ak("key"))),
			mul(opt(as("arg"))),
		},
	},
	// --- container commands (no direct arguments) ---
	"client": {
		summary: "A container for client connection commands", since: "2.4.0", group: "connection",
	},
	"monitor": {
		summary: "Listen for all requests received by the server", since: "1.0.0", group: "server",
	},
	"latency": {
		summary: "A container for latency diagnostics commands", since: "2.8.12", group: "server",
	},
	"memory": {
		summary: "A container for memory diagnostics commands", since: "4.0.0", group: "server",
	},
	"slowlog": {
		summary: "Manage the slow queries log", since: "2.2.12", group: "server",
	},
	"command": {
		summary: "A container for Redis commands", since: "2.8.13", group: "server",
	},
}

// ---------------------------------------------------------------------------
// Serialization
// ---------------------------------------------------------------------------

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

// docToReply serializes a cmdDoc into a flat key-value array matching
// Redis 7's COMMAND DOCS RESP2 wire format:
//
//	[summary, <s>, since, <v>, group, <g>, complexity, <c>, arguments, [...]]
func docToReply(doc cmdDoc) redis.Reply {
	pairs := make([]redis.Reply, 0, 12)
	pairs = append(pairs,
		protocol.MakeBulkReply([]byte("summary")),
		protocol.MakeBulkReply([]byte(doc.summary)),
		protocol.MakeBulkReply([]byte("since")),
		protocol.MakeBulkReply([]byte(doc.since)),
		protocol.MakeBulkReply([]byte("group")),
		protocol.MakeBulkReply([]byte(doc.group)),
	)
	if doc.complexity != "" {
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("complexity")),
			protocol.MakeBulkReply([]byte(doc.complexity)),
		)
	}
	if len(doc.arguments) > 0 {
		argReplies := make([]redis.Reply, len(doc.arguments))
		for i, a := range doc.arguments {
			argReplies[i] = argToReply(a)
		}
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("arguments")),
			protocol.MakeMultiRawReply(argReplies),
		)
	}
	return protocol.MakeMultiRawReply(pairs)
}

// argToReply recursively serializes a cmdArg into a flat key-value array
// matching Redis 7's COMMAND DOCS argument format.
//
// Each argument is emitted as alternating key-value pairs:
//
//	name  <name>
//	type  <type>
//	display_text  <displayText>   (if set)
//	token         <token>         (if set)
//	key_spec_index <n>            (if >= 0)
//	since         <version>       (if set)
//	flags         [...]           (if optional or multiple or explicit flags)
//	arguments     [...]           (if subArgs present)
func argToReply(a cmdArg) redis.Reply {
	pairs := make([]redis.Reply, 0, 16)
	pairs = append(pairs,
		protocol.MakeBulkReply([]byte("name")),
		protocol.MakeBulkReply([]byte(a.name)),
		protocol.MakeBulkReply([]byte("type")),
		protocol.MakeBulkReply([]byte(a.typ)),
	)
	if a.displayText != "" {
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("display_text")),
			protocol.MakeBulkReply([]byte(a.displayText)),
		)
	}
	if a.token != "" {
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("token")),
			protocol.MakeBulkReply([]byte(a.token)),
		)
	}
	if a.hasKeySpecIdx {
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("key_spec_index")),
			protocol.MakeIntReply(int64(a.keySpecIdx)),
		)
	}
	if a.since != "" {
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("since")),
			protocol.MakeBulkReply([]byte(a.since)),
		)
	}
	// Build flags list. In Redis 7, "optional" and "multiple" appear as flag strings.
	var flags []string
	if a.optional {
		flags = append(flags, "optional")
	}
	if a.multiple {
		flags = append(flags, "multiple")
	}
	flags = append(flags, a.flags...)
	if len(flags) > 0 {
		flagReplies := make([]redis.Reply, len(flags))
		for i, f := range flags {
			flagReplies[i] = protocol.MakeStatusReply(f)
		}
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("flags")),
			protocol.MakeMultiRawReply(flagReplies),
		)
	}
	if len(a.subArgs) > 0 {
		subReplies := make([]redis.Reply, len(a.subArgs))
		for i, sub := range a.subArgs {
			subReplies[i] = argToReply(sub)
		}
		pairs = append(pairs,
			protocol.MakeBulkReply([]byte("arguments")),
			protocol.MakeMultiRawReply(subReplies),
		)
	}
	return protocol.MakeMultiRawReply(pairs)
}
