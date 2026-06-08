package database

import "sync"

type cmdStat struct {
	calls    int64
	usec     int64
	failed   int64
	rejected int64
	latHisto [16]int64 // log2 usec buckets for latencystats percentiles
}

type cmdStats struct {
	mu   sync.Mutex
	cmds map[string]*cmdStat
	errs map[string]int64 // error-prefix -> count
}

func newCmdStats() *cmdStats {
	return &cmdStats{
		cmds: make(map[string]*cmdStat),
		errs: make(map[string]int64),
	}
}

func (s *cmdStats) record(name string, usec int64, isErr bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cmds[name]
	if c == nil {
		c = &cmdStat{}
		s.cmds[name] = c
	}
	c.calls++
	c.usec += usec
	if isErr {
		c.failed++
	}
	c.latHisto[log2Bucket(usec)]++
}

func (s *cmdStats) recordError(prefix string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.errs[prefix]++
	s.mu.Unlock()
}

func (s *cmdStats) snapshot() map[string]cmdStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := make(map[string]cmdStat, len(s.cmds))
	for k, v := range s.cmds {
		snap[k] = *v
	}
	return snap
}

func (s *cmdStats) errorStats() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := make(map[string]int64, len(s.errs))
	for k, v := range s.errs {
		snap[k] = v
	}
	return snap
}

func (s *cmdStats) reset() {
	s.mu.Lock()
	s.cmds = make(map[string]*cmdStat)
	s.errs = make(map[string]int64)
	s.mu.Unlock()
}

// log2Bucket returns a bucket index 0..15 for the given microseconds.
// Uses integer log2: bucket = min(15, floor(log2(usec+1)))
func log2Bucket(usec int64) int {
	if usec <= 0 {
		return 0
	}
	v := usec
	bucket := 0
	for v > 1 && bucket < 15 {
		v >>= 1
		bucket++
	}
	return bucket
}

// errorPrefix extracts the first word after '-' from an error reply.
// e.g., "-ERR unknown command" -> "ERR"
func errorPrefix(b []byte) string {
	// Find the first '-' at start
	i := 0
	for i < len(b) && b[i] == '-' {
		i++
	}
	start := i
	for i < len(b) && b[i] != ' ' && b[i] != '\r' && b[i] != '\n' {
		i++
	}
	return string(b[start:i])
}
