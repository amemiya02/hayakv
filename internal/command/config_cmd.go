package database

import (
	"strconv"
	"strings"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/wildcard"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// execConfig implements CONFIG GET/SET/REWRITE/RESETSTAT.
func execConfig(s *Server, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeArgNumErrReply("config")
	}
	sub := strings.ToUpper(string(args[0]))
	switch sub {
	case "GET":
		if len(args) < 2 {
			return protocol.MakeArgNumErrReply("config|get")
		}
		param := strings.ToLower(string(args[1]))
		pairs := configGet(param)
		return protocol.MakeMultiBulkReply(pairs)
	case "SET":
		if len(args) < 3 {
			return protocol.MakeArgNumErrReply("config|set")
		}
		param := strings.ToLower(string(args[1]))
		value := string(args[2])
		return s.configSet(param, value)
	case "REWRITE":
		return configRewrite()
	case "RESETSTAT":
		if s != nil {
			s.cmdStats.reset()
			s.slogLogger.Reset()
		}
		return protocol.MakeOkReply()
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
		"hash-max-listpack-value":   func() string { return strconv.Itoa(config.Properties.HashMaxListpackValue) },
		"set-max-intset-entries":    func() string { return strconv.Itoa(config.Properties.SetMaxIntsetEntries) },
		"set-max-listpack-entries":  func() string { return strconv.Itoa(config.Properties.SetMaxListpackEntries) },
		"set-max-listpack-value":    func() string { return strconv.Itoa(config.Properties.SetMaxListpackValue) },
		"zset-max-listpack-entries": func() string { return strconv.Itoa(config.Properties.ZSetMaxListpackEntries) },
		"zset-max-listpack-value":   func() string { return strconv.Itoa(config.Properties.ZSetMaxListpackValue) },
		"notify-keyspace-events":    func() string { return config.Properties.NotifyKeyspaceEvents },
		"latency-monitor-threshold": func() string { return "0" },
		"slowlog-log-slower-than":   func() string { return strconv.FormatInt(config.Properties.SlowLogSlowerThan, 10) },
		"slowlog-max-len":           func() string { return strconv.Itoa(config.Properties.SlowLogMaxLen) },
		"port":                      func() string { return strconv.Itoa(config.Properties.Port) },
		"bind":                      func() string { return config.Properties.Bind },
		"requirepass":               func() string { return config.Properties.RequirePass },
		"maxclients":                func() string { return strconv.Itoa(config.Properties.MaxClients) },
		"dbfilename":                func() string { return config.Properties.RDBFilename },
		"dir":                       func() string { return config.Properties.Dir },
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

func (s *Server) configSet(param, value string) redis.Reply {
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
	case "slowlog-log-slower-than":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'slowlog-log-slower-than'")
		}
		config.Properties.SlowLogSlowerThan = n
		if s != nil && s.slogLogger != nil {
			s.slogLogger.SetThreshold(n)
		}
	case "slowlog-max-len":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET 'slowlog-max-len'")
		}
		config.Properties.SlowLogMaxLen = n
		if s != nil && s.slogLogger != nil {
			s.slogLogger.SetMaxLen(n)
		}
	case "hash-max-listpack-entries", "hash-max-listpack-value",
		"set-max-intset-entries", "set-max-listpack-entries", "set-max-listpack-value",
		"zset-max-listpack-entries", "zset-max-listpack-value", "list-max-listpack-size":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return protocol.MakeErrReply("ERR Invalid argument '" + value + "' for CONFIG SET '" + param + "'")
		}
		switch param {
		case "hash-max-listpack-entries":
			config.Properties.HashMaxListpackEntries = n
		case "hash-max-listpack-value":
			config.Properties.HashMaxListpackValue = n
		case "set-max-intset-entries":
			config.Properties.SetMaxIntsetEntries = n
		case "set-max-listpack-entries":
			config.Properties.SetMaxListpackEntries = n
		case "set-max-listpack-value":
			config.Properties.SetMaxListpackValue = n
		case "zset-max-listpack-entries":
			config.Properties.ZSetMaxListpackEntries = n
		case "zset-max-listpack-value":
			config.Properties.ZSetMaxListpackValue = n
		case "list-max-listpack-size":
			config.Properties.ListMaxListpackSize = n
		}
		ApplyEncodingThresholds()
	default:
		// Unknown-but-tolerated: Redis accepts many params we don't model.
		return protocol.MakeOkReply()
	}
	return protocol.MakeOkReply()
}

// ApplyEncodingThresholds pushes the current *-max-listpack-* config values
// into the object package, where encoding conversions read them. Called at
// startup (via server.NewStorageEngine) and after CONFIG SET.
func ApplyEncodingThresholds() {
	object.SetEncodingThresholds(object.EncodingThresholds{
		HashMaxListpackEntries: config.Properties.HashMaxListpackEntries,
		HashMaxListpackValue:   config.Properties.HashMaxListpackValue,
		SetMaxIntsetEntries:    config.Properties.SetMaxIntsetEntries,
		SetMaxListpackEntries:  config.Properties.SetMaxListpackEntries,
		SetMaxListpackValue:    config.Properties.SetMaxListpackValue,
		ZSetMaxListpackEntries: config.Properties.ZSetMaxListpackEntries,
		ZSetMaxListpackValue:   config.Properties.ZSetMaxListpackValue,
		ListMaxListpackSize:    config.Properties.ListMaxListpackSize,
	})
}

func validPolicy(v string) bool {
	switch strings.ToLower(v) {
	case "noeviction", "allkeys-lru", "allkeys-lfu", "allkeys-random",
		"volatile-lru", "volatile-lfu", "volatile-random", "volatile-ttl":
		return true
	}
	return false
}
