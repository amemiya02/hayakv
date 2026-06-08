package bench

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

// BenchmarkMemoryStringKeys measures allocations for creating many small string values.
func BenchmarkMemoryStringKeys(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		keys := make([][]byte, 10000)
		for j := 0; j < 10000; j++ {
			keys[j] = []byte(fmt.Sprintf("key:%d:value:%d", j, j))
		}
	}
}

// BenchmarkMemoryIntKeys measures allocations for integer-only data.
func BenchmarkMemoryIntKeys(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		keys := make([]int64, 10000)
		for j := 0; j < 10000; j++ {
			keys[j] = int64(j)
		}
	}
}

// TestMemoryReport prints Go runtime memory statistics.
// Run with: go test ./test/bench -run TestMemoryReport -v
func TestMemoryReport(t *testing.T) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("Alloc      = %d MB", m.Alloc/1024/1024)
	t.Logf("TotalAlloc = %d MB", m.TotalAlloc/1024/1024)
	t.Logf("Sys        = %d MB", m.Sys/1024/1024)
	t.Logf("NumGC      = %d", m.NumGC)

	gogc := os.Getenv("GOGC")
	if gogc == "" {
		gogc = "100 (default)"
	}
	t.Logf("GOGC       = %s", gogc)

	gomemlimit := os.Getenv("GOMEMLIMIT")
	if gomemlimit == "" {
		gomemlimit = "unset"
	}
	t.Logf("GOMEMLIMIT = %s", gomemlimit)
}
