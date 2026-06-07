package aof

import (
	"fmt"
	"strconv"
	"strings"
)

// Manifest file/type constants (Redis multi-part AOF).
const (
	AOFManifestTypeBase = "b" // base file (RDB preamble or plain AOF)
	AOFManifestTypeIncr = "i" // incremental AOF
	AOFManifestTypeHist = "h" // history (to be removed)

	ManifestSuffix = ".manifest"
	BaseRDBSuffix  = ".base.rdb"
	BaseAOFSuffix  = ".base.aof"
	IncrAOFSuffix  = ".incr.aof"
)

// ManifestEntry is one line of the manifest.
type ManifestEntry struct {
	FileName string
	Seq      int64
	Type     string
}

// Manifest is the parsed appendonly.aof.manifest.
type Manifest struct {
	Files []ManifestEntry
}

// ParseManifest parses manifest bytes; every non-empty line must be
// "file <name> seq <n> type <b|i|h>". Unknown/short lines are an error.
func ParseManifest(data []byte) (*Manifest, error) {
	m := &Manifest{}
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		toks := strings.Fields(line)
		if len(toks) != 6 || toks[0] != "file" || toks[2] != "seq" || toks[4] != "type" {
			return nil, fmt.Errorf("manifest line %d malformed: %q", lineNo+1, line)
		}
		seq, err := strconv.ParseInt(toks[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("manifest line %d bad seq %q: %w", lineNo+1, toks[3], err)
		}
		typ := toks[5]
		if typ != AOFManifestTypeBase && typ != AOFManifestTypeIncr && typ != AOFManifestTypeHist {
			return nil, fmt.Errorf("manifest line %d bad type %q", lineNo+1, typ)
		}
		m.Files = append(m.Files, ManifestEntry{FileName: toks[1], Seq: seq, Type: typ})
	}
	return m, nil
}

// Serialize renders the manifest back to its on-disk text form.
func (m *Manifest) Serialize() string {
	var b strings.Builder
	for _, e := range m.Files {
		fmt.Fprintf(&b, "file %s seq %d type %s\n", e.FileName, e.Seq, e.Type)
	}
	return b.String()
}

// Base returns the active base entry (last type=b), or nil.
func (m *Manifest) Base() *ManifestEntry {
	for i := len(m.Files) - 1; i >= 0; i-- {
		if m.Files[i].Type == AOFManifestTypeBase {
			return &m.Files[i]
		}
	}
	return nil
}

// Incrs returns the active incr entries in order.
func (m *Manifest) Incrs() []ManifestEntry {
	var out []ManifestEntry
	for _, e := range m.Files {
		if e.Type == AOFManifestTypeIncr {
			out = append(out, e)
		}
	}
	return out
}

// maxIncrSeq returns the highest incr seq seen (0 if none).
func (m *Manifest) maxIncrSeq() int64 {
	var max int64
	for _, e := range m.Files {
		if e.Type == AOFManifestTypeIncr && e.Seq > max {
			max = e.Seq
		}
	}
	return max
}
