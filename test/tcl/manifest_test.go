package tcl

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manifestEntry represents one row in manifest.yaml.
type manifestEntry struct {
	File   string
	Status string
	Reason string
	Skips  []string
}

// loadManifest parses manifest.yaml and returns a map keyed by file path.
// It uses a minimal hand-rolled parser to avoid an external dependency.
func loadManifest(t *testing.T, path string) map[string]manifestEntry {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	defer f.Close()

	m := make(map[string]manifestEntry)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- {") && !strings.HasPrefix(trimmed, "-{") {
			continue
		}
		// Strip leading "- " and trailing "}"
		inner := strings.TrimPrefix(trimmed, "- ")
		inner = strings.TrimPrefix(inner, "-")
		inner = strings.TrimSpace(inner)
		if !strings.HasPrefix(inner, "{") || !strings.HasSuffix(inner, "}") {
			continue
		}
		inner = inner[1 : len(inner)-1] // strip { }

		entry := manifestEntry{}
		// Parse key: value pairs separated by commas
		pairs := splitKV(inner)
		for _, kv := range pairs {
			parts := strings.SplitN(kv, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			switch key {
			case "file":
				entry.File = val
			case "status":
				entry.Status = val
			case "reason":
				entry.Reason = val
			case "skips":
				// parse list like [a, b, c]
				val = strings.Trim(val, "[]")
				for _, s := range strings.Split(val, ",") {
					s = strings.TrimSpace(s)
					s = strings.Trim(s, "\"")
					if s != "" {
						entry.Skips = append(entry.Skips, s)
					}
				}
			}
		}
		if entry.File != "" {
			m[entry.File] = entry
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return m
}

// splitKV splits "key: val, key2: val2" respecting quoted strings that may
// contain commas.
func splitKV(s string) []string {
	var result []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' {
			inQuote = !inQuote
			current.WriteByte(ch)
		} else if ch == ',' && !inQuote {
			result = append(result, strings.TrimSpace(current.String()))
			current.Reset()
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, strings.TrimSpace(current.String()))
	}
	return result
}

// in-scope directory globs. Files matching any of these are considered
// in-scope unless they also match an exclusion pattern.
var inScopeGlobs = []string{
	"tests/unit/type/*.tcl",
	"tests/unit/*.tcl",
	"tests/integration/*.tcl",
}

// excludedDirs are entire directories skipped during discovery.
var excludedDirs = []string{
	"tests/unit/moduleapi",
	"tests/sentinel",
	"tests/cluster",
	"tests/support",
	"tests/tmp",
}

// discoverInScopeFiles walks the upstream redis tests directory and returns
// relative paths (using "/" separators) for every .tcl file that matches
// the in-scope globs and is not in an excluded directory.
func discoverInScopeFiles(t *testing.T, redisTestsDir string) []string {
	t.Helper()

	var files []string
	for _, glob := range inScopeGlobs {
		matches, err := filepath.Glob(filepath.Join(redisTestsDir, glob))
		if err != nil {
			t.Fatalf("glob %s: %v", glob, err)
		}
		for _, abs := range matches {
			rel, err := filepath.Rel(redisTestsDir, abs)
			if err != nil {
				t.Fatalf("rel: %v", err)
			}
			// Normalize to forward slashes for cross-platform comparison.
			rel = filepath.ToSlash(rel)

			// Skip files in excluded directories.
			excluded := false
			for _, ed := range excludedDirs {
				if strings.HasPrefix(rel, ed+"/") || rel == ed {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}

			files = append(files, rel)
		}
	}

	// Deduplicate (a file could match multiple globs, e.g. tests/unit/foo.tcl
	// is both tests/unit/*.tcl and not in tests/unit/type/*.tcl).
	seen := make(map[string]bool)
	var unique []string
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	return unique
}

// TestTCLManifestCoversEveryInScopeFile is the coverage gate: it verifies
// that every in-scope TCL file from the upstream redis/tests tree appears
// in manifest.yaml with a valid status.
func TestTCLManifestCoversEveryInScopeFile(t *testing.T) {
	dir := os.Getenv("REDIS_TCL_DIR")
	if dir == "" {
		t.Skip("set REDIS_TCL_DIR to the checked-out redis tests dir")
	}

	manifestPath := filepath.Join("manifest.yaml")
	m := loadManifest(t, manifestPath)

	inScope := discoverInScopeFiles(t, dir)
	if len(inScope) == 0 {
		t.Fatal("no in-scope files discovered; check REDIS_TCL_DIR")
	}

	for _, f := range inScope {
		e, ok := m[f]
		if !ok {
			t.Errorf("in-scope file %s missing from manifest.yaml", f)
			continue
		}
		switch e.Status {
		case "pass":
			// OK
		case "partial":
			if len(e.Skips) == 0 {
				t.Errorf("%s partial but lists no skipped tests + reasons", f)
			}
		case "excluded":
			if e.Reason == "" {
				t.Errorf("%s excluded without a reason", f)
			}
		default:
			t.Errorf("%s invalid status %q", f, e.Status)
		}
	}

	// Also verify every manifest entry corresponds to an actual file.
	// This catches stale entries pointing at files that no longer exist.
	for f := range m {
		abs := filepath.Join(dir, f)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			fmt.Printf("WARNING: manifest entry %s does not exist in redis tests (stale?)\n", f)
		}
	}
}

// TestManifestNoUnexplainedGaps ensures every manifest entry is either pass,
// partial with documented skips, or excluded with a reason. No entry should
// have an unexplained status.
func TestManifestNoUnexplainedGaps(t *testing.T) {
	manifestPath := filepath.Join("manifest.yaml")
	m := loadManifest(t, manifestPath)

	for f, e := range m {
		switch e.Status {
		case "pass":
			// OK
		case "partial":
			if len(e.Skips) == 0 {
				t.Errorf("%s is partial but lists no skipped tests", f)
			}
			if e.Reason == "" {
				t.Errorf("%s is partial but has no reason", f)
			}
		case "excluded":
			if e.Reason == "" {
				t.Errorf("%s is excluded but has no reason", f)
			}
		default:
			t.Errorf("%s has invalid status %q", f, e.Status)
		}
	}
}
