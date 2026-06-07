package aof

import "os"

// rewrite produces a new RDB base + fresh empty incr at the next seq and swaps
// the manifest atomically. eng provides the snapshot; dbCount is config.Databases.
// Returns the new manifest. Old files are left on disk (history); callers may GC.
func (mp *multiPart) rewrite(eng rdbEngine, dbCount int) (*Manifest, error) {
	if err := mp.ensureDir(); err != nil {
		return nil, err
	}
	var prevSeq int64
	if mp.exists() {
		if old, err := mp.loadManifest(); err == nil {
			if old.Base() != nil && old.Base().Seq > prevSeq {
				prevSeq = old.Base().Seq
			}
			if s := old.maxIncrSeq(); s > prevSeq {
				prevSeq = s
			}
		}
	}
	newSeq := prevSeq + 1

	// 1) write the new RDB base to a temp file, then rename into place
	baseName := mp.baseRDBName(newSeq)
	baseTmp, err := os.CreateTemp(mp.dir, "*.base.tmp")
	if err != nil {
		return nil, err
	}
	baseTmpName := baseTmp.Name()
	if err := DumpEngineToRDB(eng, dbCount, baseTmp); err != nil {
		_ = baseTmp.Close()
		_ = os.Remove(baseTmpName)
		return nil, err
	}
	if err := baseTmp.Sync(); err != nil {
		_ = baseTmp.Close()
		_ = os.Remove(baseTmpName)
		return nil, err
	}
	if err := baseTmp.Close(); err != nil {
		_ = os.Remove(baseTmpName)
		return nil, err
	}
	if err := os.Rename(baseTmpName, mp.pathOf(baseName)); err != nil {
		return nil, err
	}

	// 2) create the fresh (empty) incr file at the new seq
	incrName := mp.incrName(newSeq)
	incrFile, err := os.OpenFile(mp.pathOf(incrName), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	_ = incrFile.Close()

	// 3) atomically swap the manifest to point at the new base + incr
	newManifest := &Manifest{Files: []ManifestEntry{
		{FileName: baseName, Seq: newSeq, Type: AOFManifestTypeBase},
		{FileName: incrName, Seq: newSeq, Type: AOFManifestTypeIncr},
	}}
	if err := mp.writeManifest(newManifest); err != nil {
		return nil, err
	}
	return newManifest, nil
}
