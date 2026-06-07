# hayakv

![ci](https://github.com/amemiya02/hayakv/actions/workflows/ci.yml/badge.svg)
![license](https://img.shields.io/badge/license-GPL--3.0-blue)
![go](https://img.shields.io/badge/go-1.24%2B-00ADD8)

> English version: [README.md](./README.md)

**hayakv** 是一个用 Go 编写的 Redis 兼容键值服务器。日语中はや（*haya*）是快的意思。，*hayakv* 的拼写也与 はやく（*hayaku*）相近，意为“快速”，并且 KV 结尾。

这是一个学习项目：目标是通过亲手忠实重写来理解 Redis 内核——数据结构、编码、网络模型、
协议、持久化、复制与集群，对标 [Redis 8.x](https://github.com/redis/redis)。
优先级依次为：**正确性 → 可读性 → 性能**。验收标准是与真实 Redis 8.x 的
**逐字节回复一致**，由差分测试工具强制保证。

## 特性

- **数据类型** — string、list、hash、set、sorted set、bitmap、GEO、pub/sub、
  事务（`MULTI`/`WATCH`）
- **忠实的对象编码** — `int` / `embstr` / `raw`、`listpack`、`intset` 等，
  `OBJECT ENCODING` 与真实 Redis 一致
- **RESP2 + RESP3** — 通过 `HELLO` 协商 RESP3
- **两种网络模型** — goroutine-per-connection，或基于裸 `epoll` / `kqueue` 的
  单线程事件循环
- **两种存储引擎** — 分片并发 map，或与真实 Redis 一致的单 `dict` + 增量 rehash
- **过期与淘汰** — 采样式主动过期；`maxmemory` 支持 LRU / LFU / random / TTL 策略
- **持久化** — multi-part AOF（Redis 7 manifest 布局）、RDB、混合持久化、
  非阻塞 `BGSAVE`
- **复制** — `PSYNC` 全量与部分重同步、无盘复制、`WAIT`、副本提升
- **集群** — Redis Cluster 协议（`CLUSTER MEET`、槽位归属、`MOVED`/`ASK` 重定向、
  gossip 总线），另有基于 Raft 的代理集群

## 架构

服务器按层拆分，每层通过 Go 接口（"seam"）隔离，定义在
[`internal/iface/seams.go`](./internal/iface/seams.go)。每个 seam 都有两套实现——
朴素的 Go 基线实现与忠实于 Redis 的重写实现——通过配置在运行时切换，
便于在同一测试语料上做 A/B 对比：

| 配置项 | 可选值 | Seam |
|---|---|---|
| `net` | `goroutine` \| `eventloop` | 网络模型 |
| `engine` | `shardmap` \| `redisdb` | 存储引擎 |
| `proto-max` | `resp2` \| `resp3` | 协议上限 |

## 快速开始

```bash
# 构建
go build ./cmd/hayakv

# 运行 — 从工作目录读取 ./redis.conf（自带配置监听 :6399），
# 或通过 CONFIG 环境变量指定配置文件
go run ./cmd/hayakv
CONFIG=my.conf go run ./cmd/hayakv
```

使用任意 Redis 客户端连接：

```bash
redis-cli -p 6399 ping        # PONG
```

全部配置项见 [example.conf](./example.conf)。

## 目录结构

```
cmd/hayakv/        入口 — 加载配置，组装各 seam
config/            redis.conf 兼容的配置解析器
internal/
  iface/           seam 接口定义 (seams.go) — 建议先读这里
  server/          工厂：把配置映射到各 seam 实现
  net/             goroutine / eventloop 网络后端
  proto/           RESP2 / RESP3 编解码器
  command/         命令表 + 处理函数
  object/          Robj + 忠实编码（listpack、intset 等）
  datastruct/      dict, list, set, sortedset, bitmap
  persist/         AOF + RDB
  rediscluster/    Redis Cluster 协议（gossip、槽位、MOVED/ASK）
  cluster/         基于 Raft 的代理集群
test/
  integration/     redis-cli / go-redis 连接测试
  diff/            与真实 Redis 8.x 的差分测试工具
```

## 测试

```bash
go test -race ./...          # 单元 + seam 测试（开启竞态检测）
go test ./test/integration   # redis-cli + go-redis 连接测试
```

**差分测试工具**将同一组命令分别发送到 hayakv 和真实 Redis 8.x，逐字节比较回复。
它会自动通过 Docker 启动 Redis，也可以指向已有实例：

```bash
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff
```

Docker 和 `HAYAKV_DIFF_REDIS_ADDR` 都不可用时，测试会自动跳过。

## 不在范围内

Redis 8 自带的模块体系——JSON、查询引擎（全文 + 向量）、TimeSeries 以及概率数据结构
（Bloom/Cuckoo/CMS/Top-K/t-digest）——属于另一类子系统，不在本项目目标内。

## 致谢

hayakv 最初 fork 自 [HDT3213/godis](https://github.com/HDT3213/godis)——一个出色的
Go 语言 Redis 实现。衷心感谢其作者，没有它就没有这个项目。

## 许可证

[GPL-3.0](./LICENSE)。
