# hayakv

![license](https://img.shields.io/badge/license-GPL--3.0-blue)
![go](https://img.shields.io/badge/go-1.24%2B-00ADD8)
![status](https://img.shields.io/badge/milestone-M0-informational)

> English version: [README.md](./README.md)

**hayakv** 是一个用 Go 语言编写的 Redis 兼容键值服务器，基于
[HDT3213/godis](https://github.com/HDT3213/godis) 构建，并逐步深化为对生产级
[Redis 8.x](https://github.com/redis/redis) 的忠实重新实现。

它首先是一个**学习项目**：目标是通过亲手实现来*理解 Redis 内核*——数据结构、编码、
网络模型、协议、持久化和集群。优先级依次为：**正确性 → 可读性 → 性能**。

## 设计理念

hayakv 采用**绞杀者无花果架构（Strangler-Fig）**。服务器按层拆分，每层通过 Go
接口（"seam"）隔离。每个 seam 首先使用经过验证的 **godis 实现**（保证服务器始终
可运行），之后再替换为**忠实于 Redis 的**实现——可通过运行时配置切换，支持 A/B 对比。

| Seam | godis 基线 | Redis 忠实目标 |
|---|---|---|
| **NetServer** | goroutine-per-connection | 单线程事件循环（裸 `epoll`/`kqueue`） |
| **ProtocolCodec** | RESP2 | RESP2 + RESP3 (`HELLO`) |
| **StorageEngine** | 分片 map + 分片锁 | 单 `dict` + 增量 rehash + 过期 dict |
| **Object/Encoding** | Go 原生值 | `int`/`embstr`/`raw`, `listpack`, `intset`, `quicklist`, `skiplist`, `hashtable` |

**验收标准**是与真实 Redis 8.x 的逐字节行为一致，由差分测试工具验证（见[测试](#测试)）。

## 项目状态

**M0（基线）已完成：** godis 已导入并迁移到
`github.com/amemiya02/hayakv`，重组为 `cmd/` + `internal/` 布局，定义了四个 seam
并接入 godis 实现，同时完成差分测试工具、A/B 配置开关和 CI。

因此 godis 基线支持的所有功能现在都可以使用：string、list、hash、set、sorted set、
bitmap、TTL、pub/sub、GEO、事务（`MULTI`/`WATCH`）、AOF + RDB，以及基于 Raft 的
服务端集群。

**路线图：** `M1` RESP3/`HELLO` · `M2` `dict` + 增量 rehash · `M3` 真实 `[]byte`
编码 · `M4` 单线程事件循环 · `M5` 过期与驱逐 · `M6` RDB/AOF ·
`M7` 复制（`PSYNC`） · `M8` Redis Cluster 协议。

## 快速开始

```bash
# 构建
go build ./cmd/hayakv

# 运行 — 从工作目录读取 ./redis.conf（默认监听 :6399），
# 或通过 CONFIG 环境变量指定配置文件
go run ./cmd/hayakv
CONFIG=my.conf go run ./cmd/hayakv
```

使用任意 Redis 客户端连接：

```bash
redis-cli -p 6399 ping        # PONG
```

```go
rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6399", Protocol: 2})
```

### A/B 后端切换

在配置文件中设置以下选项（参见 [example.conf](./example.conf)）。M0 仅提供
godis 基线值，其他值将在对应里程碑落地后启用。

| 配置项 | 可选值 | M0 默认值 |
|---|---|---|
| `net` | `goroutine` \| `eventloop` *（M4）* | `goroutine` |
| `engine` | `shardmap` \| `redisdb` *（M2）* | `shardmap` |
| `proto-max` | `resp2` \| `resp3` *（M1）* | `resp2` |

## 目录结构

```
cmd/hayakv/        入口 — 加载配置，组装各 seam
config/            redis.conf 兼容的配置解析器
internal/
  iface/           四个 seam 接口定义 (seams.go)
  net/             NetServer 实现（goroutine；后续 eventloop）
  proto/           ProtocolCodec 实现（resp2；后续 resp3）
  command/         命令表 + 处理函数（godis database 层）
  datastruct/      dict, list, set, sortedset, bitmap, …
  persist/         AOF + RDB
  cluster/         基于 Raft 的服务端集群
  lib/             logger, utils, wildcard, …
test/
  integration/     redis-cli / go-redis 连接测试
  diff/            与真实 Redis 8.x 的差分测试工具
```

## 测试

```bash
go test -race ./...          # 单元 + seam 测试（开启竞态检测）
go test ./test/integration   # redis-cli + go-redis 连接测试
```

**差分测试工具**将一组命令分别发送到 hayakv 和真实 Redis 8.x，逐字节比较返回结果。
它会自动通过 Docker 启动 Redis，也可以指向已有的 Redis 实例：

```bash
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff
```

如果 Docker 和 `HAYAKV_DIFF_REDIS_ADDR` 都不可用，测试会自动跳过。

## 不在范围内

Redis 8 内置模块——JSON、查询引擎（全文 + 向量）、TimeSeries 和概率数据结构
（Bloom/Cuckoo/CMS/Top-K/t-digest）——属于独立的子系统类别，**不在本项目目标内**。

## 许可证

hayakv 使用 **GPL-3.0** 许可证。由于复用了
[HDT3213/godis](https://github.com/HDT3213/godis)（GPL-3.0）的代码，它属于衍生作品，
继续使用 GPL-3.0。原始 godis 版权声明已保留——见 [LICENSE](./LICENSE) 和
[NOTICE](./NOTICE)。
