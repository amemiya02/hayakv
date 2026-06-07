package database

import (
	"strconv"
	"strings"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/wildcard"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// execConfig implements a minimal CONFIG GET/SET sufficient for the eviction/expiry differential
// scenarios and for go-redis/redis-py to negotiate eviction settings.
func execConfig(args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeArgNumErrReply("config")
	}
	sub := strings.ToUpper(string(args[0]))
	switch sub {
	case "GET":
		param := strings.ToLower(string(args[1]))
		pairs := configGet(param)
		return protocol.MakeMultiBulkReply(pairs)
	case "SET":
		if len(args) < 3 {
			return protocol.MakeArgNumErrReply("config|set")
		}
		param := strings.ToLower(string(args[1]))
		value := string(args[2])
		return configSet(param, value)
	default:
		return protocol.MakeErrReply("ERR Unknown CONFIG subcommand or wrong number of arguments for '" + sub + "'")
	}
}

// configGet returns name/value byte pairs for the (possibly wildcard) param.
// Supports glob patterns (e.g. "maxmemory*", "*") via internal/lib/wildcard.
func configGet(param string) [][]byte {
	known := map[string]func() string{
		"maxmemory":                 func() string { return strconv.FormatInt(config.Properties.Maxmemory, 10) },
		"maxmemory-policy":          func() string { return config.Properties.MaxmemoryPolicy },
		"maxmemory-samples":         func() string { return strconv.Itoa(config.Properties.MaxmemorySamples) },
		"hz":                        func() string { return strconv.Itoa(config.Properties.Hz) },
		"appendonly":                func() string { return boolToYesNo(config.Properties.AppendOnly) },
		"databases":                 func() string { return strconv.Itoa(config.Properties.Databases) },
		"save":                      func() string { return "" },
		"list-max-listpack-size":    func() string { return strconv.Itoa(config.Properties.ListMaxListpackSize) },
		"hash-max-listpack-entries": func() string { return strconv.Itoa(config.Properties.HashMaxListpackEntries) },
		"set-max-intset-entries":    func() string { return strconv.Itoa(config.Properties.SetMaxIntsetEntries) },
		"zset-max-listpack-entries": func() string { return strconv.Itoa(config.Properties.ZSetMaxListpackEntries) },
		"notify-keyspace-events":    func() string { return config.Properties.NotifyKeyspaceEvents },
	}

	// Build the matching predicate. If param contains a wildcard character, use
	// the compiled wildcard matcher; otherwise do an exact match (fast path).
	var matches func(string) bool
	if strings.ContainsAny(param, "*?[]") {
		pat, err := wildcard.CompilePattern(param)
		if err != nil {
			return nil
		}
		matches = pat.IsMatch
	} else {
		matches = func(name string) bool { return name == param }
	}

	var out [][]byte
	for name, getter := range known {
		if matches(name) {
			out = append(out, []byte(name), []byte(getter()))
		}
	}
	return out
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func configSet(param, value string) redis.Reply {
	switch param {
	case "maxmemory":
		n, err := config.ParseMemoryBytes(value)
		if err != nil {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'maxmemory'")
		}
		config.Properties.Maxmemory = n
	case "maxmemory-policy":
		if !validPolicy(value) {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'maxmemory-policy'")
		}
		config.Properties.MaxmemoryPolicy = strings.ToLower(value)
	case "maxmemory-samples":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'maxmemory-samples'")
		}
		config.Properties.MaxmemorySamples = n
	case "hz":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'hz'")
		}
		config.Properties.Hz = n
	case "appendonly":
		switch strings.ToLower(value) {
		case "yes", "1":
			config.Properties.AppendOnly = true
		case "no", "0":
			config.Properties.AppendOnly = false
		default:
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'appendonly'")
		}
	case "notify-keyspace-events":
		if !validNotifyFlags(value) {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'notify-keyspace-events'")
		}
		config.Properties.NotifyKeyspaceEvents = value
	default:
		// Unknown-but-tolerated: Redis accepts many params we don't model.
		return protocol.MakeOkReply()
	}
	return protocol.MakeOkReply()
}

func validPolicy(v string) bool {
	switch strings.ToLower(v) {
	case "noeviction", "allkeys-lru", "allkeys-lfu", "allkeys-random",
		"volatile-lru", "volatile-lfu", "volatile-random", "volatile-ttl":
		return true
	}
	return false
}
