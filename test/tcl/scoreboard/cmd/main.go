// Command cmd reads the manifest and diff corpus metadata, computes a KPI
// Record, and appends it to history.jsonl. Intended to be run by the nightly
// CI workflow after the TCL suite completes.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amemiya02/hayakv/test/tcl/scoreboard"
)

func main() {
	// Locate repo root (run from anywhere inside the repo).
	repoRoot := findRepoRoot()

	// --- TCL pass rate from manifest ---
	manifestPath := filepath.Join(repoRoot, "test/tcl/manifest.yaml")
	pass, partial, excluded := countManifestStatuses(manifestPath)
	total := pass + partial + excluded
	passRate := 0.0
	if total > 0 {
		passRate = float64(pass) / float64(total)
	}

	// --- Diff corpus scenario count ---
	corpusDir := filepath.Join(repoRoot, "test/diff")
	scenarios := countCorpusScenarios(corpusDir)

	// --- Diff exclusion count ---
	coveragePath := filepath.Join(repoRoot, "test/diff/coverage_test.go")
	exclusions := countDiffExclusions(coveragePath)

	// --- Commands implemented (from router.go) ---
	commandsPath := filepath.Join(repoRoot, "internal/command/router.go")
	commands := countCommands(commandsPath)

	rec := scoreboard.Record{
		Date:                time.Now().UTC().Format("2006-01-02"),
		TCLPassRate:         passRate,
		TCLFilesPass:        pass,
		TCLFilesPartial:     partial,
		TCLFilesExcluded:    excluded,
		CommandsImplemented: commands,
		ConfigParams:        0, // TODO: count CONFIG params when config registry exists
		DiffCorpusScenarios: scenarios,
		DiffExclusions:      exclusions,
	}

	historyPath := filepath.Join(repoRoot, "test/tcl/scoreboard/history.jsonl")
	if err := scoreboard.Append(historyPath, rec); err != nil {
		fmt.Fprintf(os.Stderr, "append: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(rec.JSONLine())
}

func findRepoRoot() string {
	// Walk up looking for go.mod.
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fmt.Fprintln(os.Stderr, "cannot find repo root (go.mod)")
			os.Exit(1)
		}
		dir = parent
	}
}

func countManifestStatuses(path string) (pass, partial, excluded int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "status:") {
			continue
		}
		if strings.Contains(line, "pass") && !strings.Contains(line, "partial") {
			pass++
		} else if strings.Contains(line, "partial") {
			partial++
		} else if strings.Contains(line, "excluded") {
			excluded++
		}
	}
	return
}

func countCorpusScenarios(dir string) int {
	count := 0
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "corpus_") || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		count += strings.Count(string(data), "Args:")
	}
	return count
}

func countDiffExclusions(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	// Each exclusion line looks like `"name": "reason",`
	count := 0
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "diffExclusions") && strings.Contains(trimmed, "map[string]string{") {
			inBlock = true
			continue
		}
		if inBlock {
			if trimmed == "}" {
				break
			}
			if strings.HasPrefix(trimmed, "\"") {
				count++
			}
		}
	}
	return count
}

func countCommands(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	// Rough heuristic: count lines that look like command table entries.
	// This will be refined when a proper command registry exists.
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "\"") && strings.Contains(trimmed, ":") {
			count++
		}
	}
	return count
}
