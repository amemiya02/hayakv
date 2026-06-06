# hayakv

![license](https://img.shields.io/badge/license-GPL--3.0-blue)
![go](https://img.shields.io/badge/go-1.24%2B-00ADD8)
![status](https://img.shields.io/badge/milestone-M0-informational)

> 中文版见 [README_CN.md](./README_CN.md)

**hayakv** is a Redis-compatible key-value server written in Go, built on top of
[HDT3213/godis](https://github.com/HDT3213/godis) and progressively deepened toward a
faithful reimplementation of production [Redis 8.x](https://github.com/redis/redis).

It is first and foremost a **learning project**: the goal is to *understand the Redis kernel*
— its data structures, encodings, network model, protocol, persistence, and clustering — by
implementing them by hand. Priorities, in order: **correctness → readability → performance**.

## Design philosophy

hayakv uses a **strangler-fig architecture**. The server is split into layers, and each layer
is isolated behind a Go interface ("seam"). Every seam ships first with the proven **godis
implementation** (so the server always runs), and is later swapped for a **Redis-faithful**
implementation — selectable at runtime for side-by-side A/B comparison.

| Seam | godis baseline | Redis-faithful target |
|---|---|---|
| **NetServer** | goroutine-per-connection | single-threaded event loop (bare `epoll`/`kqueue`) |
| **ProtocolCodec** | RESP2 | RESP2 + RESP3 (`HELLO`) |
| **StorageEngine** | sharded map + sharded locks | single `dict` + incremental rehash + expiry dict |
| **Object/Encoding** | Go-native values | `int`/`embstr`/`raw`, `listpack`, `intset`, `quicklist`, `skiplist`, `hashtable` |

The **acceptance bar** is byte-for-byte behavioral parity with real Redis 8.x, verified by a
differential test harness (see [Testing](#testing)).

## Project status

**M0 (baseline) is complete:** godis imported and re-homed under
`github.com/amemiya02/hayakv`, restructured into `cmd/` + `internal/`, the four seams defined
with godis implementations behind them, plus a differential test harness, A/B config switches,
and CI.

Everything the godis baseline supports therefore works today: strings, lists, hashes, sets,
sorted sets, bitmaps, TTL, pub/sub, GEO, transactions (`MULTI`/`WATCH`), AOF + RDB, and
Raft-based server-side clustering.

**Roadmap:** `M1` RESP3/`HELLO` · `M2` `dict` + incremental rehash · `M3` real `[]byte`
encodings · `M4` single-threaded event loop · `M5` expiry & eviction · `M6` RDB/AOF ·
`M7` replication (`PSYNC`) · `M8` Redis Cluster protocol.

## Getting started

```bash
# build
go build ./cmd/hayakv

# run — reads ./redis.conf from the working dir (bundled config listens on :6399),
# or set CONFIG to point elsewhere
go run ./cmd/hayakv
CONFIG=my.conf go run ./cmd/hayakv
```

Connect with any Redis client:

```bash
redis-cli -p 6399 ping        # PONG
```

```go
rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6399", Protocol: 2})
```

### A/B backend switches

Set these in your config file (see [example.conf](./example.conf)). Only the godis-baseline
values are available in M0; the others light up as their milestones land.

| Key | Values | M0 default |
|---|---|---|
| `net` | `goroutine` \| `eventloop` *(M4)* | `goroutine` |
| `engine` | `shardmap` \| `redisdb` *(M2)* | `shardmap` |
| `proto-max` | `resp2` \| `resp3` *(M1)* | `resp2` |

## Layout

```
cmd/hayakv/        entry point — loads config, assembles the seams
config/            redis.conf-compatible config parser
internal/
  iface/           the four seam interfaces (seams.go)
  net/             NetServer implementations (goroutine; eventloop later)
  proto/           ProtocolCodec implementations (resp2; resp3 later)
  command/         command table + handlers (godis database layer)
  datastruct/      dict, list, set, sortedset, bitmap, …
  persist/         AOF + RDB
  cluster/         Raft-backed server-side cluster
  lib/             logger, utils, wildcard, …
test/
  integration/     redis-cli / go-redis connectivity
  diff/            differential harness vs real Redis 8.x
```

## Testing

```bash
go test -race ./...          # unit + seam tests (race detector on)
go test ./test/integration   # redis-cli + go-redis connectivity
```

The **differential harness** replays a command corpus against both hayakv and a real Redis 8.x
and compares replies byte-for-byte. It runs Redis via Docker automatically, or point it at an
existing instance:

```bash
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff
```

If neither Docker nor `HAYAKV_DIFF_REDIS_ADDR` is available, the harness skips cleanly.

## Out of scope

The Redis 8 bundled module universe — JSON, the query engine (full-text + vector), TimeSeries,
and the probabilistic types (Bloom/Cuckoo/CMS/Top-K/t-digest) — is a separate class of
subsystem and **not a goal** of this project.

## License

hayakv is licensed under **GPL-3.0**. Because it reuses code from
[HDT3213/godis](https://github.com/HDT3213/godis) (GPL-3.0), it is a derivative work and
remains GPL-3.0. The original godis copyright notice is preserved — see [LICENSE](./LICENSE)
and [NOTICE](./NOTICE).
