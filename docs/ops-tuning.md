# Operational Tuning: GOGC and GOMEMLIMIT

This guide covers Go runtime memory tuning for hayakv deployments. It explains how Go's garbage collector differs from Redis's memory model and provides concrete recommendations for production workloads.

## Go vs Redis Memory Fundamentals

### Go's GC model

Go uses a concurrent, tri-color mark-sweep garbage collector. Key properties:

- **Heap with headroom**: Go's GC targets `GOGC%` extra heap beyond the live set. The default `GOGC=100` means the GC allows the heap to grow to 2x the live set before triggering a collection.
- **Concurrent but CPU-costly**: GC runs concurrently with application goroutines, but still consumes CPU. Higher `GOGC` means fewer collections (less GC CPU) at the cost of higher peak RSS.
- **No fork COW**: Go does not fork child processes. This matters for persistence (see below).

### Redis's memory model

Redis uses jemalloc and relies on `fork()` for background persistence:

- **Fork + copy-on-write (COW)**: `BGSAVE` forks the server process. The child shares all parent pages via COW. Only pages modified after the fork get copied. This is extremely memory-efficient — a 10 GB dataset can be persisted with minimal extra memory.
- **jemalloc**: Redis uses jemalloc with per-thread arenas, which tends to have low fragmentation under Redis's allocation patterns.

### Why this matters for hayakv

hayakv is written in Go. It cannot use `fork()` for persistence. The implications:

| Aspect | Redis | hayakv (Go) |
|---|---|---|
| BGSAVE memory | COW — near-zero overhead | Full dataset read in-process |
| AOF rewrite memory | COW — near-zero overhead | Temporarily doubles memory usage |
| Allocator | jemalloc (low fragmentation) | Go's built-in (higher fragmentation under mixed alloc/free) |
| GC CPU | None (manual refcount in some cases) | Concurrent mark-sweep, tuned by GOGC |

## GOGC and GOMEMLIMIT

### GOGC

`GOGC` controls how aggressively the GC runs. It is the percentage growth in live heap that triggers a new GC cycle.

- `GOGC=100` (default): GC triggers when the heap reaches 2x the live set after the last collection.
- `GOGC=200`: GC triggers at 3x the live set. Fewer collections, less GC CPU, higher RSS.
- `GOGC=50`: GC triggers at 1.5x the live set. More frequent collections, more GC CPU, lower RSS.
- `GOGC=off`: Disables GC entirely. Only useful for short-lived processes.

### GOMEMLIMIT (Go 1.19+)

`GOMEMLIMIT` sets a soft memory ceiling. When the heap approaches this limit, GC runs more aggressively regardless of `GOGC`.

- Prevents OOM when memory spikes (e.g., during AOF rewrite).
- Does not guarantee the process stays under the limit — it is a soft target.
- Set it to your container or system memory limit minus headroom for non-heap uses (stacks, goroutines, kernel buffers).

## Concrete Recommendations

### Cache workloads (volatile data, can afford loss)

```
GOGC=200
GOMEMLIMIT=<2x expected live heap>
```

This doubles the GC threshold, reducing GC CPU by approximately 30-50% at the cost of higher RSS. Good for read-heavy caches where latency matters more than memory footprint.

### Durable workloads (AOF enabled, data matters)

```
GOGC=100
GOMEMLIMIT=<1.5x expected live heap>
```

Keep the default GC frequency for predictable memory usage. `GOMEMLIMIT` prevents OOM during AOF rewrite spikes, which can temporarily double the in-process memory footprint.

## How to Estimate Live Heap

### Via redis-cli

hayakv exposes memory stats through `INFO memory`:

```bash
redis-cli -p 6399 INFO memory
```

Look at `used_memory` — this approximates the Go heap.

### Via pprof

Enable the pprof HTTP endpoint (if configured in your build) and fetch a heap profile:

```bash
go tool pprof http://localhost:PORT/debug/pprof/heap
```

In the pprof interactive shell, use `top` to see the largest allocations.

## Tuning Procedure

1. **Start with defaults**: `GOGC=100`, no `GOMEMLIMIT`.
2. **Measure baseline**: Run your workload and record RSS and ops/sec. On Linux, check `/proc/<pid>/status` for `VmRSS` or use `ps aux`.
3. **Set GOMEMLIMIT**: Set it to 1.5x the observed RSS. This gives headroom for AOF rewrite spikes without risking OOM.
4. **Increase GOGC incrementally**: Try `GOGC=150`, then `GOGC=200`. At each step, measure:
   - Peak RSS (should stay under GOMEMLIMIT)
   - P99 latency (should improve or stay stable)
   - GC CPU (visible in `GODEBUG=gctrace=1` output)
5. **Find the sweet spot**: The highest GOGC where latency is acceptable and RSS stays under GOMEMLIMIT.

## Runtime Debugging

### GC trace

```bash
GODEBUG=gctrace=1 ./hayakv
```

This logs GC cycles to stderr. Each line shows:

```
gc N @Ss P: total-NMB cpu-Nms/Nms clock-Nms/Nms cpu-NMB/NMB goal-NMB
```

Key fields: `total` (heap after GC), `cpu` (GC CPU time), `goal` (target heap for next GC).

### Heap profile

```bash
go tool pprof http://localhost:PORT/debug/pprof/heap
```

Interactive commands: `top`, `list <function>`, `web` (requires graphviz).

### Memory stats from INFO

```bash
redis-cli -p 6399 INFO memory
```

Fields to watch:
- `used_memory`: Current heap usage
- `used_memory_rss`: Resident set size (includes non-heap)
- `used_memory_peak`: High-water mark

## Structural Differences from Redis

| Feature | Redis | hayakv (Go) |
|---|---|---|
| BGSAVE | `fork()` + COW, near-zero overhead | Reads full dataset in-process, doubles memory temporarily |
| AOF rewrite | `fork()` + COW | In-process, temporary memory spike equal to dataset size |
| Allocator | jemalloc with per-thread arenas | Go's built-in mmap-based allocator |
| Fragmentation | Low (jemalloc design) | Higher under mixed alloc/free patterns |
| GC | None (or refcount in some subsystems) | Concurrent mark-sweep, tunable via GOGC |

### Practical implications

- **Memory budget**: Plan for 2x your dataset size during AOF rewrite. Set `GOMEMLIMIT` accordingly.
- **Persistence frequency**: Frequent BGSAVE/AOF-rewrite in hayakv causes repeated memory spikes. Consider longer intervals than you would use with Redis.
- **Container limits**: Set the container memory limit to at least 2x the steady-state RSS. Set `GOMEMLIMIT` to about 80% of the container limit.
