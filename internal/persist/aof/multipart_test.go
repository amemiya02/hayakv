package aof

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amemiya02/hayakv/internal/iface/database"
)

// entityString is a test-only helper for constructing DataEntity with string data.
func entityString(s string) *database.DataEntity {
	return &database.DataEntity{Data: []byte(s)}
}

func TestMultiPartPaths(t *testing.T) {
	mp := newMultiPart("/data", "appendonlydir", "appendonly.aof")
	if mp.dir != "/data/appendonlydir" {
		t.Fatalf("dir = %q", mp.dir)
	}
	if got := mp.manifestPath(); got != "/data/appendonlydir/appendonly.aof.manifest" {
		t.Fatalf("manifestPath = %q", got)
	}
	if got := mp.baseRDBName(2); got != "appendonly.aof.2.base.rdb" {
		t.Fatalf("baseRDBName = %q", got)
	}
	if got := mp.incrName(5); got != "appendonly.aof.5.incr.aof" {
		t.Fatalf("incrName = %q", got)
	}
}

func TestMultiPartReadManifest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "appendonlydir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := "appendonly.aof.1.base.rdb"
	incr := "appendonly.aof.1.incr.aof"
	// a tiny faithful RDB base with one key, written via our encoder
	var rdbBuf bytes.Buffer
	eng := newFakeEngine()
	eng.entries["bk"] = entityString("bv")
	if err := DumpEngineToRDB(eng, 1, &rdbBuf); err != nil {
		t.Fatalf("dump base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, base), rdbBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	// incr file: one SET command in multibulk form
	incrData := []byte("*3\r\n$3\r\nSET\r\n$2\r\nik\r\n$2\r\niv\r\n")
	if err := os.WriteFile(filepath.Join(dir, incr), incrData, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "file " + base + " seq 1 type b\nfile " + incr + " seq 1 type i\n"
	if err := os.WriteFile(filepath.Join(dir, "appendonly.aof.manifest"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	mp := newMultiPart(root, "appendonlydir", "appendonly.aof")
	loaded, err := mp.loadManifest()
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if loaded.Base() == nil || loaded.Base().FileName != base {
		t.Fatalf("base not found: %+v", loaded)
	}
	if len(loaded.Incrs()) != 1 {
		t.Fatalf("incrs = %+v", loaded.Incrs())
	}
}
