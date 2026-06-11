# 学习路线图：用 Go 重写 Redis

hayakv 是一个把 Redis 8.x 内核「忠实重写一遍」的学习项目，验收标准是与真实
Redis 逐字节回复一致。这套文档以 hayakv 代码为教材，面向想理解 Redis 内部
原理的 Go 开发者。

## 怎么读

- **系统学习**：按 01 → 10 顺序读。章节按依赖排序——先协议与对象模型，
  再单机内核（网络、过期、持久化），最后分布式（复制、集群）与方法论。
- **按需查阅**：每章自成一体，开头列出前置章节。

每章五段式：本章导读 → Redis 原理 → hayakv 实现带读 → 动手验证 → 延伸阅读。
「动手验证」里的命令都可以在仓库根目录照抄执行。

## 章节

| 章 | 主题 |
|---|---|
| 01 | [RESP2/RESP3 协议与 HELLO 协商](01-resp.md) |
| 02 | [Robj 与对象编码](02-object.md) |
| 03 | dict 与增量 rehash（规划中） |
| 04 | list/hash/set/zset 的实现与编码切换（规划中） |
| 05 | 网络模型：goroutine vs 单线程事件循环（规划中） |
| 06 | 采样式过期与 maxmemory 淘汰（规划中） |
| 07 | RDB、multi-part AOF 与混合持久化（规划中） |
| 08 | PSYNC 复制（规划中） |
| 09 | Redis Cluster：slot、MOVED/ASK、gossip（规划中） |
| 10 | 差分测试：如何验证「逐字节一致」（规划中） |

## 待写章节

以下主题 hayakv 已实现但尚未成章：Lua 脚本、Streams / HyperLogLog /
BITFIELD、ACL 与 TLS、可观测性（INFO/SLOWLOG/MONITOR）。

## 范围说明

- 第 09 章讲 Redis Cluster 协议（`internal/rediscluster/`）；仓库里另有一套
  基于 Raft 的代理集群（`internal/cluster/`，godis 遗产），仅在该章末尾提及。
- Redis 8 的模块宇宙（JSON、全文/向量检索、TimeSeries、概率型结构）不在
  本项目范围内。
