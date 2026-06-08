package database

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// configRewrite serializes the live config back to the config file.
func configRewrite() redis.Reply {
	confPath := config.GetConfigFilePath()
	if confPath == "" {
		return protocol.MakeErrReply("ERR config file path not set")
	}

	// Read existing config file
	data, err := os.ReadFile(confPath)
	if err != nil {
		return protocol.MakeErrReply("ERR " + err.Error())
	}

	// Parse existing lines
	lines := strings.Split(string(data), "\n")
	existingKeys := make(map[string]int) // key -> line index
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		pivot := strings.IndexAny(trimmed, " \t")
		if pivot > 0 {
			key := strings.ToLower(trimmed[:pivot])
			existingKeys[key] = i
		}
	}

	// Build the new config values from Properties using reflection
	cfg := config.Properties
	t := reflect.TypeOf(cfg).Elem()
	v := reflect.ValueOf(cfg).Elem()
	n := t.NumField()

	for i := 0; i < n; i++ {
		field := t.Field(i)
		tag, ok := field.Tag.Lookup("cfg")
		if !ok || strings.TrimSpace(tag) == "" {
			continue
		}
		key := strings.ToLower(tag)
		value := formatConfigValue(v.Field(i), field.Type)
		if value == "" {
			continue
		}

		if idx, exists := existingKeys[key]; exists {
			lines[idx] = key + " " + value
		} else {
			lines = append(lines, key+" "+value)
		}
	}

	// Write atomically (temp + rename)
	tmpPath := confPath + ".tmp"
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return protocol.MakeErrReply("ERR " + err.Error())
	}
	if err := os.Rename(tmpPath, confPath); err != nil {
		return protocol.MakeErrReply("ERR " + err.Error())
	}

	return protocol.MakeOkReply()
}

func formatConfigValue(val reflect.Value, typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.String:
		return val.String()
	case reflect.Int, reflect.Int64:
		return fmt.Sprintf("%d", val.Int())
	case reflect.Bool:
		if val.Bool() {
			return "yes"
		}
		return "no"
	}
	return ""
}
