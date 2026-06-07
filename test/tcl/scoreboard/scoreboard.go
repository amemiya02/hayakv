// Package scoreboard provides a JSONL-based KPI record for tracking hayakv
// progress across nightly TCL runs. Each line is a self-contained JSON object
// that can be appended to history.jsonl.
package scoreboard

import (
	"encoding/json"
	"os"
)

// Record holds one nightly KPI snapshot.
type Record struct {
	Date                string  `json:"date"`
	TCLPassRate         float64 `json:"tcl_pass_rate"`
	TCLFilesPass        int     `json:"tcl_files_pass"`
	TCLFilesPartial     int     `json:"tcl_files_partial"`
	TCLFilesExcluded    int     `json:"tcl_files_excluded"`
	CommandsImplemented int     `json:"commands_implemented"`
	ConfigParams        int     `json:"config_params"`
	DiffCorpusScenarios int     `json:"diff_corpus_scenarios"`
	DiffExclusions      int     `json:"diff_exclusions"`
}

// JSONLine serialises the record as a single JSON line (no trailing newline).
func (r Record) JSONLine() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// ParseRecord deserialises a single JSON line back into a Record.
func ParseRecord(line string) (Record, error) {
	var r Record
	err := json.Unmarshal([]byte(line), &r)
	return r, err
}

// Append writes a record as a new line to the given JSONL file, creating it
// if it does not yet exist.
func Append(path string, r Record) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(r.JSONLine() + "\n")
	return err
}
