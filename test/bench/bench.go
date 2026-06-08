// Package bench drives redis-benchmark against a Redis-compatible server and
// returns per-combination ops/sec numbers.  Designed for CI nightly runs; never
// asserts thresholds, only records.
package bench

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// commands is the set of -t flags recognised by redis-benchmark.
var commands = []string{
	"SET", "GET", "INCR", "LPUSH", "RPUSH", "LPOP", "RPOP",
	"SADD", "HSET", "ZADD", "PING",
}

// pipelines and payloads form the benchmark matrix.
var (
	pipelines = []int{1, 10, 100, 1000}
	payloads  = []int{3, 256}
)

// rePerSec matches the throughput line emitted by redis-benchmark -q.
var rePerSec = regexp.MustCompile(`([\d.]+)\s+requests per second`)

// RunSuite executes the full pipeline x payload x command matrix against addr
// and returns a map keyed like "set_p1_d3" -> ops/sec.
func RunSuite(addr string) (map[string]float64, error) {
	host, port, err := splitAddr(addr)
	if err != nil {
		return nil, err
	}
	results := make(map[string]float64)
	for _, p := range pipelines {
		for _, d := range payloads {
			for _, cmd := range commands {
				key := fmt.Sprintf("%s_p%d_d%d", strings.ToLower(cmd), p, d)
				ops, err := runOne(host, port, cmd, p, d)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				results[key] = ops
			}
		}
	}
	return results, nil
}

// runOne invokes redis-benchmark for a single combination and parses ops/sec.
func runOne(host, port, cmd string, pipeline, payload int) (float64, error) {
	args := []string{
		"-h", host,
		"-p", port,
		"-n", "100000",
		"-P", strconv.Itoa(pipeline),
		"-d", strconv.Itoa(payload),
		"-t", cmd,
		"-q",
	}
	out, err := exec.Command("redis-benchmark", args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("redis-benchmark %s: %w\n%s", cmd, err, out)
	}
	return parseOpsPerSec(string(out))
}

// parseOpsPerSec extracts the throughput from redis-benchmark -q output.
func parseOpsPerSec(output string) (float64, error) {
	// -q prints one line per test: "SET: 123456.78 requests per second"
	// Some versions also print a summary; we take the first match.
	m := rePerSec.FindStringSubmatch(output)
	if m == nil {
		return 0, fmt.Errorf("no 'requests per second' in output:\n%s", output)
	}
	return strconv.ParseFloat(m[1], 64)
}

// splitAddr splits "host:port" into (host, port).
func splitAddr(addr string) (string, string, error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return "", "", fmt.Errorf("invalid addr %q (expected host:port)", addr)
	}
	return addr[:i], addr[i+1:], nil
}
