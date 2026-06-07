package config

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/amemiya02/hayakv/internal/lib/utils"

	"github.com/amemiya02/hayakv/internal/lib/logger"
)

var (
	ClusterMode    = "cluster"
	StandaloneMode = "standalone"
)

// ServerProperties defines global config properties
type ServerProperties struct {
	// for Public configuration
	RunID             string `cfg:"runid"` // runID always different at every exec.
	Bind              string `cfg:"bind"`
	Port              int    `cfg:"port"`
	Dir               string `cfg:"dir"`
	AnnounceHost      string `cfg:"announce-host"`
	AppendOnly        bool   `cfg:"appendonly"`
	AppendFilename    string `cfg:"appendfilename"`
	AppendDirname     string `cfg:"appenddirname"` // multi-part AOF directory (Redis 7+)
	AppendFsync       string `cfg:"appendfsync"`
	AofUseRdbPreamble bool   `cfg:"aof-use-rdb-preamble"`
	MaxClients        int    `cfg:"maxclients"`
	RequirePass       string `cfg:"requirepass"`
	Databases         int    `cfg:"databases"`
	RDBFilename       string `cfg:"dbfilename"`
	RdbImpl           string `cfg:"rdb-impl"` // "faithful" (own codec) or "library" (hdt3213/rdb)
	MasterAuth        string `cfg:"masterauth"`
	SlaveAnnouncePort int    `cfg:"slave-announce-port"`
	SlaveAnnounceIP   string `cfg:"slave-announce-ip"`
	ReplTimeout       int    `cfg:"repl-timeout"`
	ReplBacklogSize   int64  `cfg:"repl-backlog-size"`
	ReplDisklessSync  bool   `cfg:"repl-diskless-sync"`
	UseGnet           bool   `cfg:"use-gnet"`

	NetBackend    string `cfg:"net"`
	EngineBackend string `cfg:"engine"`
	ProtoMax      string `cfg:"proto-max"`

	SlowLogSlowerThan int64 `cfg:"slowlog-log-slower-than"`
	SlowLogMaxLen     int   `cfg:"slowlog-max-len"`

	ClusterEnable bool   `cfg:"cluster-enable"`
	ClusterAsSeed bool   `cfg:"cluster-as-seed"`
	ClusterSeed   string `cfg:"cluster-seed"`
	// Cluster flavor selector: "redis" => real Redis Cluster (internal/rediscluster);
	// anything else (incl. empty) => legacy raft proxy (internal/cluster) for back-compat.
	ClusterMode       string `cfg:"cluster-mode"`
	ClusterConfigFile string `cfg:"cluster-config-file"`
	RaftListenAddr    string `cfg:"raft-listen-address"`
	RaftAdvertiseAddr string `cfg:"raft-advertise-address"`
	// If the node join the cluster as a replica of another node,
	// set MasterInCluster as the RedisAdvertiseAddr of it's master node
	MasterInCluster string `cfg:"master-in-cluster"`

	// Encoding thresholds
	HashMaxListpackEntries int `cfg:"hash-max-listpack-entries"`
	HashMaxListpackValue   int `cfg:"hash-max-listpack-value"`
	SetMaxIntsetEntries    int `cfg:"set-max-intset-entries"`
	SetMaxListpackEntries  int `cfg:"set-max-listpack-entries"`
	SetMaxListpackValue    int `cfg:"set-max-listpack-value"`
	ZSetMaxListpackEntries int `cfg:"zset-max-listpack-entries"`
	ZSetMaxListpackValue   int `cfg:"zset-max-listpack-value"`
	ListMaxListpackSize    int `cfg:"list-max-listpack-size"`

	// M5: expiration + eviction
	Maxmemory        int64  `cfg:"maxmemory"`         // bytes; 0 = unlimited. Accepts 100mb/1gb forms via fixup.
	MaxmemoryPolicy  string `cfg:"maxmemory-policy"`  // noeviction|allkeys-lru|allkeys-lfu|allkeys-random|volatile-lru|volatile-lfu|volatile-random|volatile-ttl
	MaxmemorySamples int    `cfg:"maxmemory-samples"` // eviction pool sample size (Redis default 5)
	Hz               int    `cfg:"hz"`                // serverCron frequency; ticks every 1000/hz ms (Redis default 10)

	rawConfig map[string]string // populated by parse(); used by normalizeMemoryConfig
}

var configFilePath string

func GetConfigFilePath() string {
	return configFilePath
}

type ServerInfo struct {
	StartUpTime time.Time
}

func (p *ServerProperties) AnnounceAddress() string {
	if p.AnnounceHost != "" {
		return p.AnnounceHost + ":" + strconv.Itoa(p.Port)
	}
	return p.Bind + ":" + strconv.Itoa(p.Port)
}

func (p *ServerProperties) RaftAnnounceAddress() string {
	if p.RaftAdvertiseAddr != "" {
		return p.RaftAdvertiseAddr
	}
	return p.RaftListenAddr
}

// Properties holds global config properties
var Properties *ServerProperties
var EachTimeServerInfo *ServerInfo

func init() {
	// A few stats we don't want to reset: server startup time, and peak mem.
	EachTimeServerInfo = &ServerInfo{
		StartUpTime: time.Now(),
	}

	// default config
	Properties = &ServerProperties{
		Bind:           "127.0.0.1",
		Port:           6379,
		AppendOnly:     false,
		RunID:          utils.RandString(40),
		NetBackend:     "goroutine",
		EngineBackend:  "shardmap",
		ProtoMax:       "resp2",
		RdbImpl:        "library",
		AppendFilename: "appendonly.aof",
		AppendDirname:  "appendonlydir",

		ReplBacklogSize:  1024 * 1024, // 1MB, Redis default
		ReplDisklessSync: false,

		// Encoding thresholds (Redis defaults)
		HashMaxListpackEntries: 128,
		HashMaxListpackValue:   64,
		SetMaxIntsetEntries:    512,
		SetMaxListpackEntries:  128,
		SetMaxListpackValue:    64,
		ZSetMaxListpackEntries: 128,
		ZSetMaxListpackValue:   64,
		ListMaxListpackSize:    128,

		// M5 defaults (Redis 8)
		Maxmemory:        0, // unlimited
		MaxmemoryPolicy:  "noeviction",
		MaxmemorySamples: 5,
		Hz:               10,

		ClusterConfigFile: "nodes.conf",
	}
}

func parse(src io.Reader) *ServerProperties {
	config := &ServerProperties{}

	// read config file
	rawMap := make(map[string]string)
	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " ")
		if len(trimmed) > 0 && trimmed[0] == '#' {
			continue
		}
		pivot := strings.IndexAny(line, " ")
		if pivot > 0 && pivot < len(line)-1 { // separator found
			key := line[0:pivot]
			value := strings.Trim(line[pivot+1:], " ")
			rawMap[strings.ToLower(key)] = value
		}
	}
	if err := scanner.Err(); err != nil {
		logger.Fatal(err)
	}

	// parse format
	t := reflect.TypeOf(config)
	v := reflect.ValueOf(config)
	n := t.Elem().NumField()
	for i := 0; i < n; i++ {
		field := t.Elem().Field(i)
		fieldVal := v.Elem().Field(i)
		key, ok := field.Tag.Lookup("cfg")
		if !ok || strings.TrimLeft(key, " ") == "" {
			key = field.Name
		}
		value, ok := rawMap[strings.ToLower(key)]
		if ok {
			// fill config
			switch field.Type.Kind() {
			case reflect.String:
				fieldVal.SetString(value)
			case reflect.Int:
				intValue, err := strconv.ParseInt(value, 10, 64)
				if err == nil {
					fieldVal.SetInt(intValue)
				}
			case reflect.Int64:
				intValue, err := strconv.ParseInt(value, 10, 64)
				if err == nil {
					fieldVal.SetInt(intValue)
				}
			case reflect.Bool:
				boolValue := "yes" == value
				fieldVal.SetBool(boolValue)
			case reflect.Slice:
				if field.Type.Elem().Kind() == reflect.String {
					slice := strings.Split(value, ",")
					fieldVal.Set(reflect.ValueOf(slice))
				}
			}
		}
	}
	config.rawConfig = rawMap
	return config
}

// SetupConfig read config file and store properties into Properties
func SetupConfig(configFilename string) {
	file, err := os.Open(configFilename)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	Properties = parse(file)
	normalizeMemoryConfig(Properties)
	Properties.RunID = utils.RandString(40)
	configFilePath, err = filepath.Abs(configFilename)
	if err != nil {
		return
	}
	if Properties.Dir == "" {
		Properties.Dir = "."
	}
}

func GetTmpDir() string {
	return Properties.Dir + "/tmp"
}

// parseMemoryBytes parses Redis maxmemory forms: a plain integer (bytes), or a
// number with a unit suffix. Binary suffixes kb/mb/gb are 1024-based; metric
// suffixes k/m/g are 1000-based (matching redis.conf semantics).
func parseMemoryBytes(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, nil
	}
	mults := []struct {
		suffix string
		mult   int64
	}{
		{"kb", 1024}, {"mb", 1024 * 1024}, {"gb", 1024 * 1024 * 1024},
		{"k", 1000}, {"m", 1000 * 1000}, {"g", 1000 * 1000 * 1000}, {"b", 1},
	}
	for _, m := range mults {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(s, m.suffix))
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, err
			}
			return n * m.mult, nil
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

// ParseMemoryBytes is the exported wrapper of parseMemoryBytes for external callers.
func ParseMemoryBytes(s string) (int64, error) {
	return parseMemoryBytes(s)
}

// normalizeMemoryConfig re-parses the raw maxmemory token (the reflection parser
// only understands plain ints, so a "100mb" value lands as 0) and clamps hz /
// samples to sane ranges.
func normalizeMemoryConfig(cfg *ServerProperties) {
	if raw, ok := cfg.rawMaxmemory(); ok {
		if n, err := parseMemoryBytes(raw); err == nil {
			cfg.Maxmemory = n
		}
	}
	if cfg.Hz <= 0 {
		cfg.Hz = 10
	}
	if cfg.Hz > 500 {
		cfg.Hz = 500
	}
	if cfg.MaxmemorySamples <= 0 {
		cfg.MaxmemorySamples = 5
	}
	if cfg.MaxmemoryPolicy == "" {
		cfg.MaxmemoryPolicy = "noeviction"
	}
}

func (p *ServerProperties) rawMaxmemory() (string, bool) {
	if p.rawConfig == nil {
		return "", false
	}
	v, ok := p.rawConfig["maxmemory"]
	return v, ok
}
