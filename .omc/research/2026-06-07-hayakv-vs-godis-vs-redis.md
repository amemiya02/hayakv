# hayakv 竞争力调研报告:对比 godis 的优势 与 对比 Redis 8.x 的劣势

*生成时间:2026-06-07 | 来源:3 个并行研究 agent(本地代码库取证 + godis 仓库/issue 调研 + Redis 8.x 官方资料)| 置信度:高(本地事实直接取证于代码;外部事实均附来源)*

---

## 执行摘要

hayakv 是在 godis 快照(约 1.7 万行非测试 Go 代码)之上,以 strangler-fig(绞杀者)接缝架构新增约 1.65 万行代码的 Redis 8.x 兼容重写。**对比 godis**,hayakv 在协议(RESP3)、编码保真(OBJECT ENCODING)、存储引擎(增量 rehash)、网络模型(kqueue/epoll 事件循环)、内存管理(maxmemory/LRU/LFU)、持久化(multi-part AOF + 忠实 RDB v11)、复制(WAIT/diskless/部分重同步)、集群(真正的 Redis Cluster 协议)以及质量保障(逐字节差分测试门禁)九个维度全面领先——其中多项是 godis 作者明确表态"不做"或搁置多年的方向,且 godis 自 2025-09 起已停止更新。**对比真实 Redis 8.x**,hayakv 的差距是数量级的:命令面 123+17 vs 442 个命令/子命令定义、配置面约 40 vs 183 项;缺失 Lua 脚本、ACL、TLS、Streams、HyperLogLog、keyspace notifications、客户端缓存、Sentinel 等整块能力;Redis 8 已把 JSON/时序/概率类型/向量集合并入核心;在 fork COW 快照、jemalloc 级内存效率、流水线吞吐等方面还存在 Go 语言层面的结构性劣势。

---

## Part 1 — 对比 godis:hayakv 的优势

### 1.1 总体形态

| 指标 | hayakv | godis 上游 |
|---|---|---|
| Go 文件数 | 253 | 138 |
| 总 Go LOC | 43,630 | 25,453 |
| 非测试 LOC | 26,929 | 17,178 |
| 测试文件数 | 111 | 46 |
| 注册命令(唯一) | 123(+17 special,+4 仅分发:hello/config/wait/replicaof) | 119(+17 special) |
| 配置键 | ~40 | ~25 |
| CI 门禁 | gofmt、vet、race×2、集成、复制、差分(vs redis:8)、build | 仅 coverage 上传 |
| 维护状态 | 活跃开发(M0–M8 里程碑) | 最后 push 2025-09-14,最后 release v1.2.9(2023-06) |

hayakv 的根提交是 godis 代码的 squash 快照(162 文件 / 27,744 行,保留 GPL-3.0),此后 90 个提交按 M0(基线)→ M1(RESP3)→ M2(dict rehash)→ M3(编码)→ M4(事件循环)→ M5(过期/逐出)→ M6(RDB/AOF)→ M7(复制)→ M8(集群)里程碑推进(计划文档在 `docs/superpowers/plans/`)。

### 1.2 架构:接缝 vs 平铺

godis 是平铺包结构(`tcp/`、`redis/`、`database/`、`cluster/`),命令与存储耦合,无后端选择(仅一个 `use-gnet` 开关)。hayakv 在 `internal/iface/seams.go` 定义了 `NetServer` / `ProtocolCodec` / `StorageEngine` / `Object` 四个接缝接口,`internal/server/backends.go` 作为运行时工厂支持 `net=goroutine|eventloop`、`engine=shardmap|redisdb`、`proto-max=resp2|resp3` 的任意组合——**每个 godis 基线实现都保留可运行,可与 Redis 忠实实现 A/B 对照**。这是 godis 完全不具备的可演进性。

### 1.3 分项优势(均为已验证事实)

| 维度 | hayakv | godis | 证据 |
|---|---|---|---|
| **RESP3 / HELLO** | 完整 RESP3 reply 族(Null/Bool/Double/BigNumber/Map/Set/Push/Verbatim)、按连接协商、RESP2→RESP3 帧转换 | **完全没有**;lettuce 连接池因无 HELLO 无法连接(issue #251 仍开放),作者:"godis 现在还没有适配 RESP3" | `internal/proto/resp3/`;[godis#251](https://github.com/HDT3213/godis/issues/251) |
| **对象编码保真** | `Robj{Type,Encoding,Ptr}` + 真实 listpack/intset/int/embstr/raw/quicklist/skiplist,8 个阈值配置键,OBJECT ENCODING/FREQ/IDLETIME | **零编码概念**(grep 无 listpack/ziplist/intset 命中),无 OBJECT 命令;"support ziplist" issue #219 被关闭 | `internal/object/`;[godis#219](https://github.com/HDT3213/godis/issues/219) |
| **存储引擎** | `RedisDict`:双哈希表 + rehashIdx 增量 rehash(仿 Redis dict)+ DictScan 游标,property-based 测试(rapid) | 仅分片 ConcurrentDict / SimpleDict | `internal/datastruct/dict/redisdict.go` |
| **网络模型** | 单线程 reactor:kqueue(darwin)/epoll(linux)+ BLPOP/BRPOP 阻塞唤醒注册表 | goroutine-per-conn;作者明确拒绝 epoll 模型:"no plans to use the epoll model"(#208) | `internal/net/eventloop/`;[godis#208](https://github.com/HDT3213/godis/issues/208) |
| **maxmemory / 逐出** | 8 种策略(noeviction/allkeys-lru/lfu/random/volatile-*)、LRU 时钟 + LFU 对数计数/衰减、OOM 写拒绝、MEMORY USAGE | **没有**(grep 零命中);LRU/LFU issue #150 自 2023-04 悬置 | `internal/command/evict.go`;[godis#150](https://github.com/HDT3213/godis/issues/150) |
| **主动过期** | Redis 式采样循环:每轮 20 key、>25% 过期则重复,hz 可配;带 -race 测试 | 仅惰性过期 + timewheel 回调;且 timewheel 有并发 map 崩溃 bug(#233 开放) | `expire_cycle.go`;[godis#233](https://github.com/HDT3213/godis/issues/233) |
| **持久化** | multi-part AOF(Redis 7+ 目录布局 + manifest 原子切换)、忠实 RDB v11 编解码器(REDIS0011 + CRC64)、非阻塞 BGSAVE(时间点快照)、与真实 Redis 的 RDB 互载测试 | 单文件 AOF;RDB 完全依赖外部库 hdt3213/rdb;**只能通过 AOF rewrite 生成 RDB**(作者确认,#230) | `internal/persist/{aof,rdb}/`;[godis#230](https://github.com/HDT3213/godis/issues/230) |
| **复制** | 在 PSYNC 基础上新增:有界 backlog(repl-backlog-size)、断线部分重同步、WAIT、REPLCONF GETACK/ACK、REPLICAOF、diskless($EOF 帧)、忠实 INFO replication;**与真实 Redis 双向互通测试** | FULLRESYNC + 无界内存 backlog;无 WAIT/GETACK/diskless | `internal/command/replication_*.go` |
| **集群** | `internal/rediscluster/`:**真 Redis Cluster 协议**——CRC16/hash tag、16384 槽 + nodes.conf、二进制 gossip 总线(MEET/PING/PONG/FAIL、epoch、槽位图)、MOVED/ASK/CROSSSLOT 重定向、IMPORTING/MIGRATING + MIGRATE/DUMP/RESTORE、约 25 个 CLUSTER 子命令 | raft 元数据 + **服务端代理转发**(对客户端"透明",无 MOVED/ASK、无 CLUSTER 命令族、无 gossip);事务为回滚语义(与 Redis 相悖) | `internal/rediscluster/`;[godis#237](https://github.com/HDT3213/godis/issues/237) |
| **命令增量** | +blpop、brpop、object、memory、hello、config、wait、replicaof;godis 有而 hayakv 没有的:**无** | — | 命令表 diff |
| **质量保障** | 逐字节差分 harness(1,793 行):corpus 回放 vs Docker redis:8 + 归一化钩子 + **覆盖门禁**(每个注册命令必须被 corpus 覆盖或显式排除并写明理由);RDB 互载;13 项复制集成测试;3 节点集群测试;7 段 CI | 46 个普通单测;CI 仅 coverage,fixture 还是 **Redis 5** | `test/diff/`;[godis coverall.yml](https://github.com/HDT3213/godis/blob/master/.github/workflows/coverall.yml) |

### 1.4 定位差异(战略层)

- godis 作者自述项目目的是 **"just for fun"** 的教学示例([#206](https://github.com/HDT3213/godis/issues/206)),且明言 **"Redis style is not one of the goals. Godis even has a very un-redis style concurrency engine"**([#208](https://github.com/HDT3213/godis/issues/208))。
- hayakv 的目标函数恰好相反:**与 Redis 8.x 逐字节回复一致**,并以差分测试为验收门禁。两个项目在"忠实度"这一坐标轴上方向相反——hayakv 相对 godis 的优势不是"做得更多",而是**目标本身不同且有可执行的验收标准**。
- godis 还有未修复的正确性 bug:GEORADIUS 结果错误([#220](https://github.com/HDT3213/godis/issues/220),开放 2 年)、timewheel 并发崩溃(#233)、事务回滚语义(真实 Redis MULTI 从不回滚)。

### 1.5 诚实的注脚:hayakv 仍继承 godis 的部分

raft 代理集群(`internal/cluster/`,raft.go 与上游逐字节一致)、AOF 核心(320 行与上游等同)、RESP2 解析器(仅 ~39 行差异)、命令 handler 主体、`internal/lib/*`、pubsub、MULTI/EXEC 事务、GEO 命令、goroutine TCP server、gnet 残留。**GEO 与事务直接继承了 godis 的语义,godis 已知的 GEORADIUS bug(#220)是否同样存在于 hayakv 值得专项验证**(diff corpus 当前将 geo 显式排除,理由是"等待 Redis 8 精度审计")。

---

## Part 2 — 对比 Redis 8.x:hayakv 的劣势

### 2.1 数量级差距总览

| 维度 | Redis 8.x | hayakv | 差距 |
|---|---|---|---|
| 命令/子命令定义 | **442**(src/commands/,已验证;不含 FT./JSON./TS. 等) | 123 + 17 special + 4 分发 | ~3 倍 |
| 配置参数 | **183**(8.0 config.c,已验证) | ~40 | ~4.5 倍 |
| 测试规模 | TCL 套件 ~150 文件(unit 47 + type 13 + cluster 16 + moduleapi 46 + integration 28)+ sentinel 21 + 旧 cluster 33,用例数以千计 | diff corpus 数十场景 + 111 个 Go 测试文件;TCL runner 仅脚手架 | 数量级 |
| DEBUG 子命令 | **48 个**(unstable debug.c,已验证),TCL 套件重度依赖(RELOAD/DIGEST/OBJECT/SLEEP/SET-ACTIVE-EXPIRE…) | 无 DEBUG | 整块缺失 |

### 2.2 缺失的整块能力(按影响排序)

1. **脚本化**:EVAL/EVALSHA/EVAL_RO、SCRIPT 族(嵌入 Lua 5.1)+ Redis Functions(FUNCTION LOAD/FCALL,持久化进 RDB/AOF 并参与复制)。这是生态阻断项——asynq、sidekiq 类客户端因 EVALSHA 直接无法工作(godis #221 即此类报告)。([eval-intro](https://redis.io/docs/latest/develop/programmability/eval-intro/)、[functions-intro](https://redis.io/docs/latest/develop/programmability/functions-intro/))
2. **安全面**:ACL(用户/命令类别/key 模式/channel 模式/selectors)、TLS(含复制与集群总线)、protected-mode 分级。([ACL](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/))
3. **数据类型**:Streams(XADD/消费者组全家桶,8.2+ 还有 XDELEX/XACKDEL)、HyperLogLog(PFADD/PFCOUNT/PFMERGE)、BITFIELD;以及 **Redis 8 已并入核心的** JSON、时序、5 种概率类型(bloom/cuckoo/CMS/top-k/t-digest)、向量集合(VADD/VSIM,HNSW + 量化)与 Redis 查询引擎(全文/向量检索)。hayakv 把"模块"列为 out of scope,但 **8.0 起这些对用户而言就是默认构建的核心命令面**,不再是可选模块。([Redis 8 GA](https://redis.io/blog/redis-8-ga/))
4. **客户端协同**:client-side caching(CLIENT TRACKING/BCAST/RESP3 invalidation push)、keyspace notifications(`__keyspace@__`/`__keyevent@__`,8.2 新增 OVERWRITTEN/TYPE_CHANGED)、CLIENT 子命令族(ID/INFO/LIST/KILL/PAUSE/NO-EVICT/NO-TOUCH…)、MONITOR、RESET。([client-side caching](https://redis.io/docs/latest/develop/reference/client-side-caching/)、[keyspace notifications](https://redis.io/docs/latest/develop/pubsub/keyspace-notifications/))
5. **HA**:无 Sentinel 等价物(监控/通知/自动 failover/服务发现;真实套件含 21 个 Sentinel 测试文件);复制侧缺 WAITAOF(7.2)、FAILOVER(6.2)、PSYNC2 的 replid2 换主部分重同步、8.0 双通道全量同步(快照+变更流并行,快 18%)。([replication](https://redis.io/docs/latest/operate/oss_and_stack/management/replication/)、[sentinel](https://redis.io/docs/latest/operate/oss_and_stack/management/sentinel/))
6. **可观测性**:LATENCY 族、SLOWLOG、INFO 的 commandstats/latencystats/errorstats 段、MEMORY DOCTOR/STATS、OBJECT REFCOUNT、CONFIG REWRITE。
7. **过期语义细节**:hash field TTL(7.4 HEXPIRE 族 + 8.0 HGETEX/HSETEX/HGETDEL)、EXPIRE 的 NX/XX/GT/LT 选项、**replica 不自行删除过期键而由 master 传播 DEL**(主从一致性关键语义,需专项验证 hayakv 行为)。([EXPIRE](https://redis.io/docs/latest/commands/expire/))

### 2.3 集群成熟度差距

hayakv 的 rediscluster 实现了协议骨架(gossip/槽/重定向/迁移状态/约 25 个子命令),但与真实集群相比缺:**自动故障转移**(PFAIL→FAIL 多数派判定、按复制偏移排序的副本选举、epoch 投票)、**replica migration**(自动为孤儿主补副本)、CLUSTER FAILOVER [FORCE|TAKEOVER]、BUMPEPOCH/LINKS/COUNT-FAILURE-REPORTS、8.2 的 CLUSTER SLOT-STATS、**8.4 的原子槽迁移(CLUSTER MIGRATION)**——后者是需要持续跟踪的语义变更。分区下的 gossip 时序/quorum 行为、半迁移槽上的 ASK/MOVED 边界情况,是公认最难忠实复刻的部分。([cluster spec](https://redis.io/docs/latest/operate/oss_and_stack/reference/cluster-spec/)、[8.4.0 release](https://github.com/redis/redis/releases/tag/8.4.0))

### 2.4 性能与内存:Go 的结构性劣势

- **快照机制**:Redis 用 fork + COW 做 BGSAVE/diskless 同步,内存开销近乎为零增量;Go 无法安全 fork,hayakv 的时间点快照需在用户态复制可达数据,这是**架构级不可消除的差距**,只能工程化缓解。
- **内存效率**:Redis 有 jemalloc + 主动碎片整理、0–9999 共享整数对象(已验证 OBJ_SHARED_INTEGERS 10000)、SDS、listpack 紧凑编码,8.2 的 kvobj 把 key+短 value+TTL 打进单次分配(宣称最高省 67% 内存)。Go 侧有 GC 堆余量(GOGC 默认让活堆翻倍)、无 defrag、map/interface 每对象开销。**M16 已实现共享整数池(0–9999)与常用回复 intern(+OK/NullBulk/IntReply 0/1),减少了热路径分配。** ([8.2 GA](https://redis.io/blog/redis-82-ga/)、[memory optimization](https://redis.io/docs/latest/operate/oss_and_stack/management/optimization/memory-optimization/))
- **吞吐**:Redis 单线程核心 + io-threads(8.0 重做异步 IO 线程,宣称 io-threads 8 下吞吐 +112%;8.4 再 +30%)。一个 2025 年的 Go 克隆实测:非流水线 Go ~104–110k ops/s vs Redis 136k SET;**万级流水线下 Redis 2.1M–2.65M ops/s,而朴素 Go 实现停留在 ~110k**——锁竞争占 CPU ~35%、每请求 42 次分配是主因。**M16 针对性优化:零分配整数解析(parseUint)、queryBuf 压缩、输出缓冲区复用、pipeline 解码批处理。** 流水线处理与分配纪律是 Go 克隆最大的性能失分点。([jauhar.dev](https://jauhar.dev/blog/2025/02/09/intro-to-high-performance-golang-redis-clone/)、[Redis 8 GA](https://redis.io/blog/redis-8-ga/))
- 旁证:微软 Garnet(C#)证明托管语言克隆可以在吞吐上反超 Redis,但前提是放弃单线程架构 + 热路径零分配——说明差距可以工程化弥补,但代价是架构复杂度。([MSR blog](https://www.microsoft.com/en-us/research/blog/introducing-garnet-an-open-source-next-generation-faster-cache-store-for-accelerating-applications-and-services/))

### 2.5 持久化格式差距

- hayakv 忠实 RDB 实现已升级为 **v12(REDIS0012)**,对齐 Redis 8.x。redis/redis unstable 的 RDB_VERSION 已是 **14**(已验证 src/rdb.h;7.x/8.0 为 11–12)。做主从互通与 DEBUG RELOAD 兼容时需持续跟版本。
- **M16 已对齐**:Redis 默认 `aof-use-rdb-preamble yes`(rewrite 后的 base 是 RDB 体),hayakv 默认已改为 `yes`。8.4 新增 AOF 尾部自动修复待后续跟进。([persistence](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/))

### 2.6 测试覆盖的"已知未知"

hayakv 自己的差分排除清单(~113 条)如实记录了尚未被逐字节验证的面:**事务(multi/exec/watch)、pub/sub、GEO(等待精度审计)、阻塞命令、auth、scan 游标顺序**等。这些命令"存在但未验证与 Redis 一致"——相对 Redis 的劣势不仅是缺失的命令,还包括**已实现但保真度未证明**的命令。这是 hayakv 质量体系的优点(显式记录),也是当前的真实差距。

### 2.7 M16 已关闭的差距项

| 原始差距 | M16 关闭方式 | 验证 |
|---------|-------------|------|
| 无共享整数对象 | `internal/object/shared.go` — 0–9999 共享 Robj 池 | `TestSharedIntegers` |
| 常用回复每次分配 | `internal/proto/resp2/protocol/interned.go` — NullBulk/EmptyMulti/IntReply(0,1) 单例 | `TestInterned*` |
| pipeline 解码每命令分配 | 零分配 `parseUint` + cmds 预分配 + queryBuf 压缩 | `BenchmarkParseRequests*` |
| 输出缓冲区不复用 | `bufconn.takeOut()` 复用 backing array | `TestBufConn*` |
| 无 8.4 SET IFEQ/IFGT | `execSet` 增加条件写入选项 | `TestSetIfEq` |
| 无 MSETEX | `execMSetEX` — 多键共享 TTL | 命令注册 |
| 无 DELEX | `execDelEX` — 删除返回计数 | `TestDelEx` |
| 无 BITOP DIFF/DIFF1/ANDOR/ONE | `execBitOp` — 8.2 位运算 | `TestBitOp*` |
| RDB 版本未对齐 | 升级为 REDIS0012 (v12) | encoder/decoder 测试 |
| aof-use-rdb-preamble 默认不符 | 默认改为 `yes` | config 默认值 |
| 无 benchmark 仪表盘 | `test/bench/` — redis-benchmark 矩阵 + scoreboard bench_* | `TestBenchVsRedis` |
| 无运维调优指南 | `docs/ops-tuning.md` — GOGC/GOMEMLIMIT 指引 | 文档 |

**仍需 M17+ 跟进的结构性差距**:fork COW、jemalloc 内存效率、io-threads 吞吐上限。

---

## 关键结论

1. **对 godis 的优势是方向性的,不只是增量的**:hayakv 在 godis 作者明确放弃的所有轴(RESP3、epoll、编码保真、Redis 风格语义、差分验收)上建立了系统性能力,且 godis 已事实停滞(最后 push 2025-09-14)。hayakv 已不是"改进版 godis",而是借 godis 起步、目标函数完全不同的项目。
2. **对 Redis 的劣势分三层**:
   - **可追赶层**(命令/配置覆盖、keyspace notifications、SLOWLOG、CLIENT 族、hash-field TTL、DEBUG 子命令):工程量问题;
   - **大工程层**(Lua/Functions、ACL/TLS、Streams、Sentinel、集群自动 failover):每项都是月级工程;
   - **结构层**(fork COW、jemalloc 内存效率、流水线吞吐、8.2 kvobj):受 Go 运行时约束,只能缓解不能消除。
3. **最高杠杆的下一步**(按解锁价值排序,供参考):
   - **DEBUG 子命令子集**(RELOAD/OBJECT/DIGEST/SLEEP/SET-ACTIVE-EXPIRE/QUICKLIST-PACKED-THRESHOLD)——直接解锁真实 Redis TCL 套件(`test/tcl/` 脚手架已就位),把验收面从自建 corpus 扩到上游数千用例;
   - **把排除清单清零**:事务、pub/sub、GEO、scan 进 diff corpus(多连接 harness + 归一化钩子);
   - **Lua(如 gopher-lua)**:单点解锁大量真实客户端/框架兼容;
   - **keyspace notifications + hash-field TTL**:面积小、客户端依赖多;
   - 持续跟踪 8.4 atomic slot migration 与 RDB 版本演进。

---

## 来源

**本地取证(hayakv@620565c vs godis 浅克隆)**:`internal/iface/seams.go`、`internal/server/backends.go`、`internal/proto/resp3/`、`internal/object/`、`internal/datastruct/dict/redisdict.go`、`internal/net/eventloop/`、`internal/command/{evict,expire_cycle,wait,replication_*}.go`、`internal/persist/{aof,rdb}/`、`internal/rediscluster/`、`test/diff/`(含 coverage_test.go 门禁与 ~113 条排除清单)、`.github/workflows/ci.yml`;godis 对照:`tcp/`、`redis/`、`database/`、`cluster/{core,raft}`、`aof/`、`.github/workflows/coverall.yml`。

**godis 外部来源**:[README](https://github.com/HDT3213/godis/blob/master/README.md) · [commands.md](https://github.com/HDT3213/godis/blob/master/commands.md) · issues [#251 RESP3](https://github.com/HDT3213/godis/issues/251)、[#221 Lua](https://github.com/HDT3213/godis/issues/221)、[#152](https://github.com/HDT3213/godis/issues/152)、[#151 CONFIG](https://github.com/HDT3213/godis/issues/151)、[#150 LRU/LFU](https://github.com/HDT3213/godis/issues/150)、[#149 Stream](https://github.com/HDT3213/godis/issues/149)、[#219 ziplist](https://github.com/HDT3213/godis/issues/219)、[#220 GEORADIUS bug](https://github.com/HDT3213/godis/issues/220)、[#233 timewheel race](https://github.com/HDT3213/godis/issues/233)、[#230 RDB](https://github.com/HDT3213/godis/issues/230)、[#237 raft 架构](https://github.com/HDT3213/godis/issues/237)、[#208 非目标声明](https://github.com/HDT3213/godis/issues/208)、[#206 初衷](https://github.com/HDT3213/godis/issues/206) · gh api 仓库元数据(3,837 stars,last push 2025-09-14)。

**Redis 8.x 来源**:[Redis 8 GA blog](https://redis.io/blog/redis-8-ga/) · [8.0 release notes](https://github.com/redis/redis/blob/8.0/00-RELEASENOTES) · [8.2 GA](https://redis.io/blog/redis-82-ga/) · [8.2 what's new](https://redis.io/docs/latest/develop/whats-new/8-2/) · [8.4.0 release](https://github.com/redis/redis/releases/tag/8.4.0) · [cluster spec](https://redis.io/docs/latest/operate/oss_and_stack/reference/cluster-spec/) · [replication](https://redis.io/docs/latest/operate/oss_and_stack/management/replication/) · [persistence](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/) · [Sentinel](https://redis.io/docs/latest/operate/oss_and_stack/management/sentinel/) · [eviction](https://redis.io/docs/latest/develop/reference/eviction/) · [EXPIRE](https://redis.io/docs/latest/commands/expire/) · [client-side caching](https://redis.io/docs/latest/develop/reference/client-side-caching/) · [ACL](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/) · [eval/functions](https://redis.io/docs/latest/develop/programmability/eval-intro/) · [keyspace notifications](https://redis.io/docs/latest/develop/pubsub/keyspace-notifications/) · [multi-part AOF 设计](https://www.alibabacloud.com/blog/design-and-implementation-of-redis-7-0-multi-part-aof_599199) · [antirez diskless](https://antirez.com/news/81) · [Go 克隆性能实测](https://jauhar.dev/blog/2025/02/09/intro-to-high-performance-golang-redis-clone/) · [Garnet](https://www.microsoft.com/en-us/research/blog/introducing-garnet-an-open-source-next-generation-faster-cache-store-for-accelerating-applications-and-services/) · redis/redis 源码直接验证:`src/rdb.h`(RDB_VERSION 14)、`src/debug.c`(48 子命令)、`src/config.c`(183 参数)、`src/server.h`(OBJ_SHARED_INTEGERS、LRU_BITS)、8.0 `redis.conf`、tests/ 目录清单。

## 方法论

3 个并行研究 agent:(1) 本地代码库取证——克隆 godis 上游做逐文件/逐命令 diff、git 历史分析、命令表抽取、测试基建盘点;(2) godis 仓库调研——README/issues/releases/CI 共 19 次工具调用;(3) Redis 8.x 调研——官方博客/发布说明/文档 + redis/redis 源码 GitHub API 直接验证,共 36 次工具调用。所有外部声明附 URL;无法证实处标注 unverified。
