package scoreboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordRoundTrips(t *testing.T) {
	rec := Record{
		Date:                "2026-06-07",
		TCLPassRate:         0.42,
		TCLFilesPass:        6,
		TCLFilesPartial:     0,
		TCLFilesExcluded:    22,
		CommandsImplemented: 124,
		ConfigParams:        41,
		DiffCorpusScenarios: 129,
		DiffExclusions:      116,
	}
	line := rec.JSONLine()

	got, err := ParseRecord(line)
	if err != nil {
		t.Fatalf("ParseRecord error: %v", err)
	}
	if got.Date != rec.Date {
		t.Errorf("Date = %q, want %q", got.Date, rec.Date)
	}
	if got.TCLPassRate != rec.TCLPassRate {
		t.Errorf("TCLPassRate = %v, want %v", got.TCLPassRate, rec.TCLPassRate)
	}
	if got.CommandsImplemented != rec.CommandsImplemented {
		t.Errorf("CommandsImplemented = %d, want %d", got.CommandsImplemented, rec.CommandsImplemented)
	}
	if got.DiffCorpusScenarios != rec.DiffCorpusScenarios {
		t.Errorf("DiffCorpusScenarios = %d, want %d", got.DiffCorpusScenarios, rec.DiffCorpusScenarios)
	}
	if got.DiffExclusions != rec.DiffExclusions {
		t.Errorf("DiffExclusions = %d, want %d", got.DiffExclusions, rec.DiffExclusions)
	}
}

func TestAppendCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	rec := Record{Date: "2026-01-01", TCLPassRate: 1.0, TCLFilesPass: 5}
	if err := Append(path, rec); err != nil {
		t.Fatalf("first Append: %v", err)
	}

	rec2 := Record{Date: "2026-01-02", TCLPassRate: 0.8, TCLFilesPass: 4}
	if err := Append(path, rec2); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Must have exactly two lines.
	lines := splitLines(string(data))
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	// Second line should be the second record.
	got, err := ParseRecord(lines[1])
	if err != nil {
		t.Fatalf("ParseRecord line 2: %v", err)
	}
	if got.Date != "2026-01-02" {
		t.Errorf("line 2 Date = %q, want %q", got.Date, "2026-01-02")
	}
}

func TestRecordCarriesBench(t *testing.T) {
	rec := Record{Date: "2026-06-07", Bench: map[string]float64{
		"set_p1_ops": 110000, "set_p100_ops": 850000, "get_p1_ops": 115000,
	}}
	got, err := ParseRecord(rec.JSONLine())
	if err != nil || got.Bench["set_p100_ops"] != 850000 {
		t.Fatalf("bench round-trip failed: %v %+v", err, got)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
