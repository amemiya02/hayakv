package database

import "strings"

// KeySpec describes where a command's keys live in its argument vector, mirroring
// commandExtra. firstKey/lastKey are 1-based arg indices (0 => no keys); lastKey<0
// means "len(args)+lastKey"; keyStep is the stride between keys.
type KeySpec struct {
	FirstKey int
	LastKey  int
	KeyStep  int
}

// LookupKeySpec returns the key spec for a command name (case-insensitive). ok is
// false for unknown commands or commands without an attached extra/keys.
func LookupKeySpec(name string) (KeySpec, bool) {
	cmd := cmdTable[strings.ToLower(name)]
	if cmd == nil || cmd.extra == nil {
		return KeySpec{}, false
	}
	return KeySpec{
		FirstKey: cmd.extra.firstKey,
		LastKey:  cmd.extra.lastKey,
		KeyStep:  cmd.extra.keyStep,
	}, true
}

// ExtractKeys returns the keys of a command line per its KeySpec. cmdLine[0] is the
// command name. Returns nil for keyless commands.
func ExtractKeys(cmdLine [][]byte) [][]byte {
	if len(cmdLine) == 0 {
		return nil
	}
	spec, ok := LookupKeySpec(string(cmdLine[0]))
	if !ok || spec.FirstKey == 0 || spec.KeyStep == 0 {
		return nil
	}
	last := spec.LastKey
	if last < 0 {
		last = len(cmdLine) + last // e.g. -1 => last arg index
	}
	var keys [][]byte
	for i := spec.FirstKey; i <= last && i < len(cmdLine); i += spec.KeyStep {
		keys = append(keys, cmdLine[i])
	}
	return keys
}
