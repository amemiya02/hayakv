package aof

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	rdbcodec "github.com/amemiya02/hayakv/internal/persist/rdb"
	"github.com/amemiya02/hayakv/internal/proto/resp2/parser"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/server/connection"

	"github.com/amemiya02/hayakv/internal/lib/logger"
)

// multiPart owns the appenddirname layout and naming for Redis 7+ AOF.
type multiPart struct {
	dir      string // <Dir>/<appenddirname>
	baseName string // e.g. "appendonly.aof" -- the naming stem
}

// newMultiPart builds the layout helper. baseDir is config Dir, dirName is
// appenddirname, baseName is appendfilename.
func newMultiPart(baseDir, dirName, baseName string) *multiPart {
	return &multiPart{dir: filepath.Join(baseDir, dirName), baseName: baseName}
}

func (mp *multiPart) manifestPath() string {
	return filepath.Join(mp.dir, mp.baseName+ManifestSuffix)
}
func (mp *multiPart) baseRDBName(seq int64) string {
	return fmt.Sprintf("%s.%d%s", mp.baseName, seq, BaseRDBSuffix)
}
func (mp *multiPart) baseAOFName(seq int64) string {
	return fmt.Sprintf("%s.%d%s", mp.baseName, seq, BaseAOFSuffix)
}
func (mp *multiPart) incrName(seq int64) string {
	return fmt.Sprintf("%s.%d%s", mp.baseName, seq, IncrAOFSuffix)
}
func (mp *multiPart) pathOf(name string) string { return filepath.Join(mp.dir, name) }

// ensureDir creates the appenddirname directory if missing.
func (mp *multiPart) ensureDir() error { return os.MkdirAll(mp.dir, 0o755) }

// exists reports whether a manifest is already present.
func (mp *multiPart) exists() bool {
	_, err := os.Stat(mp.manifestPath())
	return err == nil
}

// loadManifest reads and parses the manifest file.
func (mp *multiPart) loadManifest() (*Manifest, error) {
	data, err := os.ReadFile(mp.manifestPath())
	if err != nil {
		return nil, err
	}
	return ParseManifest(data)
}

// writeManifest atomically replaces the manifest (temp + rename in the same dir).
func (mp *multiPart) writeManifest(m *Manifest) error {
	if err := mp.ensureDir(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(mp.dir, "*.manifest.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(m.Serialize()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, mp.manifestPath())
}

// LoadMultiPart loads a multi-part AOF into db: base (RDB or AOF) then each incr.
// It is a no-op (returns nil) when no manifest exists, so first boot is clean.
func (persister *Persister) LoadMultiPart(mp *multiPart) error {
	if !mp.exists() {
		return nil
	}
	manifest, err := mp.loadManifest()
	if err != nil {
		return err
	}
	// 1) base
	if base := manifest.Base(); base != nil {
		if err := persister.loadBaseFile(mp.pathOf(base.FileName)); err != nil {
			return err
		}
	}
	// 2) incrs in order
	for _, incr := range manifest.Incrs() {
		if err := persister.loadIncrFile(mp.pathOf(incr.FileName)); err != nil {
			return err
		}
	}
	return nil
}

// loadBaseFile loads a base file. A ".base.rdb" base is parsed by the faithful
// decoder and replayed as commands; a ".base.aof" base is replayed directly.
func (persister *Persister) loadBaseFile(path string) error {
	if strings.HasSuffix(path, BaseRDBSuffix) {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		fakeConn := connection.NewFakeConn()
		dec := rdbcodec.NewDecoder(f)
		return dec.Parse(func(e rdbcodec.Entry) bool {
			fakeConn.SelectDB(e.DBIndex)
			for _, batch := range LoadEntriesAsCommands(e) {
				for _, cmd := range batch {
					persister.db.Exec(fakeConn, cmd)
				}
			}
			return true
		})
	}
	return persister.loadIncrFile(path) // plain-AOF base
}

// loadIncrFile replays a multibulk AOF file into the engine.
func (persister *Persister) loadIncrFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var reader io.Reader = f
	ch := parser.ParseStream(reader)
	fakeConn := connection.NewFakeConn()
	for p := range ch {
		if p.Err != nil {
			if p.Err == io.EOF {
				break
			}
			logger.Error("parse incr: " + p.Err.Error())
			continue
		}
		r, ok := p.Data.(*protocol.MultiBulkReply)
		if !ok {
			continue
		}
		persister.db.Exec(fakeConn, r.Args)
		if strings.ToLower(string(r.Args[0])) == "select" && len(r.Args) == 2 {
			if idx, err := strconv.Atoi(string(r.Args[1])); err == nil {
				persister.currentDB = idx
			}
		}
	}
	return nil
}
