package database

import (
	"fmt"
	"sort"
	"strings"
)

// infoCommandstats returns the # Commandstats INFO section.
func (server *Server) infoCommandstats() string {
	var b strings.Builder
	b.WriteString("# Commandstats\r\n")
	snap := server.cmdStats.snapshot()
	names := make([]string, 0, len(snap))
	for n := range snap {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := snap[n]
		per := 0.0
		if c.calls > 0 {
			per = float64(c.usec) / float64(c.calls)
		}
		fmt.Fprintf(&b, "cmdstat_%s:calls=%d,usec=%d,usec_per_call=%.2f,rejected_calls=%d,failed_calls=%d\r\n",
			n, c.calls, c.usec, per, c.rejected, c.failed)
	}
	return b.String()
}

// infoLatencystats returns the # Latencystats INFO section.
func (server *Server) infoLatencystats() string {
	var b strings.Builder
	b.WriteString("# Latencystats\r\n")
	snap := server.cmdStats.snapshot()
	names := make([]string, 0, len(snap))
	for n := range snap {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := snap[n]
		fmt.Fprintf(&b, "latency_percentiles_usec_%s:p50=%.2f,p99=%.2f,p999=%.2f\r\n",
			n, estimatePercentile(c.latHisto, 50), estimatePercentile(c.latHisto, 99), estimatePercentile(c.latHisto, 99.9))
	}
	return b.String()
}

// infoErrorstats returns the # Errorstats INFO section.
func (server *Server) infoErrorstats() string {
	var b strings.Builder
	b.WriteString("# Errorstats\r\n")
	errs := server.cmdStats.errorStats()
	names := make([]string, 0, len(errs))
	for n := range errs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "errorstat_%s:count=%d\r\n", n, errs[n])
	}
	return b.String()
}

// estimatePercentile estimates a percentile from log2 histogram buckets.
func estimatePercentile(histo [16]int64, pct float64) float64 {
	var total int64
	for _, c := range histo {
		total += c
	}
	if total == 0 {
		return 0
	}
	target := int64(float64(total) * pct / 100.0)
	var cum int64
	for i, c := range histo {
		cum += c
		if cum >= target {
			return float64(int64(1) << uint(i))
		}
	}
	return float64(int64(1) << 15)
}
