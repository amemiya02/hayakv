package aof

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amemiya02/hayakv/internal/iface/database"
)

// makeEntityString is a small helper for rewrite tests.
func makeEntityString(val string) *database.DataEntity {
	return &database.DataEntity{Data: []byte(val)}
}

func TestRewriteMultiPart(t *testing.T) {
	root := t.TempDir()
	mp := newMultiPart(root, "appendonlydir", "appendonly.aof")
	if err := mp.ensureDir(); err != nil {
		t.Fatal(err)
	}
	// seed an initial manifest with an old base + incr (empty files are fine)
	old := &Manifest{Files: []ManifestEntry{
		{FileName: mp.baseRDBName(1), Seq: 1, Type: AOFManifestTypeBase},
		{FileName: mp.incrName(1), Seq: 1, Type: AOFManifestTypeIncr},
	}}
	_ = os.WriteFile(mp.pathOf(mp.baseRDBName(1)), []byte("stale"), 0o644)
	_ = os.WriteFile(mp.pathOf(mp.incrName(1)), []byte("stale"), 0o644)
	if err := mp.writeManifest(old); err != nil {
		t.Fatal(err)
	}

	// engine with one key to dump into the new base
	eng := newFakeEngine()
	eng.entries["k"] = makeEntityString("v")

	newManifest, err := mp.rewrite(eng, 1)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	// manifest must point at seq 2 base + seq 2 incr
	base := newManifest.Base()
	if base == nil || base.Seq != 2 {
		t.Fatalf("new base seq != 2: %+v", base)
	}
	incrs := newManifest.Incrs()
	if len(incrs) != 1 || incrs[0].Seq != 2 {
		t.Fatalf("new incr seq != 2: %+v", incrs)
	}
	// new base file exists and is a real RDB (starts with REDIS0011)
	data, err := os.ReadFile(mp.pathOf(base.FileName))
	if err != nil {
		t.Fatalf("read new base: %v", err)
	}
	if string(data[:9]) != "REDIS0011" {
		t.Fatalf("new base not RDB: %q", data[:9])
	}
	// new incr file exists and is empty (fresh)
	incrPath := mp.pathOf(incrs[0].FileName)
	if fi, err := os.Stat(incrPath); err != nil || fi.Size() != 0 {
		t.Fatalf("new incr not fresh-empty: err=%v", err)
	}
	// the on-disk manifest equals what rewrite returned
	reloaded, err := mp.loadManifest()
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	if reloaded.Serialize() != newManifest.Serialize() {
		t.Fatalf("manifest not persisted atomically:\n got %q\nwant %q", reloaded.Serialize(), newManifest.Serialize())
	}
	// only one base entry should remain active (old one demoted to history or dropped)
	if got := filepath.Base(base.FileName); got != "appendonly.aof.2.base.rdb" {
		t.Fatalf("unexpected base name %q", got)
	}
}
