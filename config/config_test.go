package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	src := "bind 0.0.0.0\n" +
		"port 6399\n" +
		"appendonly yes\n" +
		"peers a,b"
	p := parse(strings.NewReader(src))
	if p == nil {
		t.Error("cannot get result")
		return
	}
	if p.Bind != "0.0.0.0" {
		t.Error("string parse failed")
	}
	if p.Port != 6399 {
		t.Error("int parse failed")
	}
	if !p.AppendOnly {
		t.Error("bool parse failed")
	}
}

func TestParseReplFields(t *testing.T) {
	src := "bind 127.0.0.1\n" +
		"port 6399\n" +
		"repl-backlog-size 2097152\n" +
		"repl-diskless-sync yes\n"
	p := parse(strings.NewReader(src))
	if p.ReplBacklogSize != 2097152 {
		t.Errorf("ReplBacklogSize = %d, want 2097152", p.ReplBacklogSize)
	}
	if !p.ReplDisklessSync {
		t.Errorf("ReplDisklessSync = false, want true")
	}
}

func TestReplBacklogSizeDefault(t *testing.T) {
	// init() sets package-global Properties; assert its default.
	if Properties.ReplBacklogSize != 1024*1024 {
		t.Errorf("default ReplBacklogSize = %d, want %d", Properties.ReplBacklogSize, 1024*1024)
	}
}

func TestParseMaxmemoryByteSuffix(t *testing.T) {
	cfg := parse(strings.NewReader("maxmemory 100mb\nmaxmemory-policy allkeys-lru\nmaxmemory-samples 7\nhz 50\n"))
	normalizeMemoryConfig(cfg)
	if cfg.Maxmemory != 100*1024*1024 {
		t.Fatalf("maxmemory = %d, want %d", cfg.Maxmemory, 100*1024*1024)
	}
	if cfg.MaxmemoryPolicy != "allkeys-lru" {
		t.Fatalf("policy = %q", cfg.MaxmemoryPolicy)
	}
	if cfg.MaxmemorySamples != 7 {
		t.Fatalf("samples = %d", cfg.MaxmemorySamples)
	}
	if cfg.Hz != 50 {
		t.Fatalf("hz = %d", cfg.Hz)
	}
}

func TestParseMaxmemoryPlainAndUnits(t *testing.T) {
	cases := map[string]int64{
		"1048576": 1048576,
		"100":     100,
		"1kb":     1024,
		"2k":      2000,
		"1gb":     1024 * 1024 * 1024,
		"5m":      5_000_000,
	}
	for in, want := range cases {
		got, err := parseMemoryBytes(in)
		if err != nil || got != want {
			t.Fatalf("parseMemoryBytes(%q) = %d,%v want %d", in, got, err, want)
		}
	}
}

func TestMemoryConfigDefaults(t *testing.T) {
	// init() should have populated package Properties with M5 defaults.
	if Properties.Hz != 10 {
		t.Fatalf("default hz = %d, want 10", Properties.Hz)
	}
	if Properties.MaxmemorySamples != 5 {
		t.Fatalf("default samples = %d, want 5", Properties.MaxmemorySamples)
	}
	if Properties.MaxmemoryPolicy != "noeviction" {
		t.Fatalf("default policy = %q, want noeviction", Properties.MaxmemoryPolicy)
	}
	if Properties.Maxmemory != 0 {
		t.Fatalf("default maxmemory = %d, want 0 (unlimited)", Properties.Maxmemory)
	}
}
