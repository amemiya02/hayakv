# hayakv

![ci](https://github.com/amemiya02/hayakv/actions/workflows/ci.yml/badge.svg)
![license](https://img.shields.io/badge/license-GPL--3.0-blue)
![go](https://img.shields.io/badge/go-1.24%2B-00ADD8)

> 中文版见 [README_CN.md](./README_CN.md)

**hayakv** is a Redis-compatible key-value server written in Go. はや (haya) means fast in Japanese — and *hayakv* reads like はやく (*hayaku*, "quickly")
with a KV ending.

It is a learning project: the goal is to understand the Redis kernel — data structures,
encodings, network model, protocol, persistence, replication, and clustering — by
reimplementing them faithfully against [Redis 8.x](https://github.com/redis/redis).
Priorities, in order: **correctness → readability → performance**. The acceptance bar is
**byte-for-byte reply parity with real Redis 8.x**, enforced by a differential test harness.

## Features

- **Data types** — strings, lists, hashes, sets, sorted sets, bitmaps, GEO, pub/sub,
  transactions (`MULTI`/`WATCH`)
- **Faithful object encodings** — `int` / `embstr` / `raw`, `listpack`, `intset`, … so
  `OBJECT ENCODING` matches real Redis
- **RESP2 + RESP3** — RESP3 negotiated via `HELLO`
- **Two network models** — goroutine-per-connection, or a single-threaded event loop on
  bare `epoll` / `kqueue`
- **Two storage engines** — sharded concurrent map, or a single `dict` with incremental
  rehash like real Redis
- **Expiry & eviction** — sampling active-expire cycle; `maxmemory` with
  LRU / LFU / random / TTL policies
- **Persistence** — multi-part AOF (Redis 7 manifest layout), RDB, hybrid preamble,
  non-blocking `BGSAVE`
- **Replication** — `PSYNC` full & partial resync, diskless sync, `WAIT`, replica promotion
- **Cluster** — Redis Cluster protocol (`CLUSTER MEET`, slot ownership, `MOVED`/`ASK`
  redirection, gossip bus), plus a Raft-based proxy cluster

## Architecture

The server is split into layers, each isolated behind a Go interface (a "seam") defined in
[`internal/iface/seams.go`](./internal/iface/seams.go). Every seam has two implementations —
a straightforward Go baseline and a Redis-faithful rewrite — selected at runtime via config,
so the two can be A/B-compared against the same test corpus:

| Config key | Values | Seam |
|---|---|---|
| `net` | `goroutine` \| `eventloop` | network model |
| `engine` | `shardmap` \| `redisdb` | storage engine |
| `proto-max` | `resp2` \| `resp3` | protocol ceiling |

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

All configuration options are documented in [example.conf](./example.conf).

## Layout

```
cmd/hayakv/        entry point — loads config, assembles the seams
config/            redis.conf-compatible config parser
internal/
  iface/           seam interfaces (seams.go) — read this first
  server/          factory wiring config → seam implementations
  net/             goroutine / eventloop network backends
  proto/           RESP2 / RESP3 codecs
  command/         command table + handlers
  object/          Robj + faithful encodings (listpack, intset, …)
  datastruct/      dict, list, set, sortedset, bitmap
  persist/         AOF + RDB
  rediscluster/    Redis Cluster protocol (gossip, slots, MOVED/ASK)
  cluster/         Raft-based proxy cluster
test/
  integration/     redis-cli / go-redis connectivity
  diff/            differential harness vs real Redis 8.x
```

## Testing

```bash
go test -race ./...          # unit + seam tests (race detector on)
go test ./test/integration   # redis-cli + go-redis connectivity
```

The **differential harness** replays a command corpus against both hayakv and a real
Redis 8.x and compares replies byte-for-byte. It launches Redis via Docker automatically,
or point it at an existing instance:

```bash
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff
```

If neither Docker nor `HAYAKV_DIFF_REDIS_ADDR` is available, the harness skips cleanly.

## Out of scope

The Redis 8 bundled module universe — JSON, the query engine (full-text + vector),
TimeSeries, and the probabilistic types (Bloom/Cuckoo/CMS/Top-K/t-digest) — is a separate
class of subsystem and not a goal of this project.

## Acknowledgements

hayakv began as a fork of [HDT3213/godis](https://github.com/HDT3213/godis), an excellent
Redis implementation in Go. Many thanks to its authors — this project would not exist
without it.

## License

[GPL-3.0](./LICENSE).
