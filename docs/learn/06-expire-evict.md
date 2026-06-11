# 第 06 章：采样式过期与 maxmemory 淘汰

> **前置章节**：第 03 章（dict 与增量 rehash）。hayakv 用一张独立的
> `ttlMap`（`ConcurrentDict`）存储所有带 TTL 的 key 的到期时刻；过期逻辑建立
> 在 03 章介绍的 dict 之上，本章会直接引用那里的分片锁概念。

---

## ① 本章导读

在 Redis 中，一个 key 设置了 TTL 之后到底发生了什么？它不会立刻消失，也不一定
等到被访问时才消失。Redis 同时运行着两条并行的删除管道：

1. **惰性删除**：命令访问 key 时先检查有没有到期，到期则就地删除再返回 nil。
2. **主动采样循环**：后台按 `hz` 节奏定期抽样，把已经过期但还没被访问到的
   "幽灵 key"清掉。

内存耗尽时，还有第三条管道：**maxmemory 淘汰**。根据配置的策略，从全量或仅有
TTL 的 key 中按 LRU / LFU / TTL / Random 挑出牺牲者驱逐。

hayakv 把这三条管道都复刻了：

- `internal/command/expire_cycle.go`：采样式主动过期
- `internal/command/evict.go`：近似 LRU / LFU 与八种淘汰策略
- `internal/command/database.go`（`GetEntity`）：惰性删除检查点
- `internal/command/server.go`（`StartCron` / `activeExpireAllDBs`）：调度入口

读完本章，你能回答：

1. 为什么 Redis 不用定时器轮或全表扫描来做主动过期？
2. "采样 + 阈值循环"的自适应设计如何保证高过期密度时的及时性？
3. LRU / LFU 的精确版本内存开销有多大？近似版本如何权衡精度与空间？
4. hayakv 没有 jemalloc，怎么估算 used memory？

---

## ② Redis 原理

### 2.1 过期的两半

Redis 的过期由两套机制**协同**保证，缺一不可。

**惰性删除（lazy expiration）**

每次读/写命令访问 key 之前，Redis 在内部先查询 key 的到期时间，若已过期则原地
删除，返回"key 不存在"。优点是零额外调度开销；缺点是永远不会被访问的 key 会
一直占着内存，直到主动过期清掉它。

**主动采样循环（active expiration）**

Redis 每隔 `1/hz` 秒触发一次 `serverCron`，其中 `databasesCron` 针对每个数据库
执行一轮主动过期：

```
loop:
  从 ttlMap 随机抽取 ACTIVE_EXPIRE_CYCLE_KEYS_PER_LOOP = 20 个 key
  删除其中已过期的
  if 已过期比例 > 25%:
      goto loop   // 继续清，说明过期密度很高
  else:
      break       // 过期稀疏，本轮停止
```

这个**自适应循环**是 Redis 过期设计里最值得玩味的一处。我们来对比三种替代方案，
看看它的动机：

| 方案 | 描述 | 缺点 |
|---|---|---|
| 定时器轮（timer wheel） | 每个 key 创建一个 timer | 百万 key = 百万 timer 对象，内存与调度开销巨大 |
| 全表扫描 | 每次 cron 遍历所有 key | O(N)，key 多时一次扫描就阻塞几百毫秒 |
| 随机采样 + 阈值循环 | 采 20 个，若过期比例高则再来一轮 | 精度受 hz 影响，但开销可控 |

采样方案的聪明之处：**当 key 的过期密度低时，一轮就够了；当大批 key 同时到期
时，循环会一直跑直到密度降到 25% 以下**，自动加大清理力度而不需要手动调参。
同时每轮只随机抽取固定数量，O(1) 的单次开销让它可以安全地嵌在 serverCron 里。

**过期精度与 hz 的关系**

Redis 的默认 `hz=10`，即每 100 ms 触发一次 serverCron。这意味着主动过期的精度
上界约为 100 ms——一个已过期的 key 最多可能在内存中多活 100 ms 后被主动清除。
调高 `hz` 可以改善精度（代价是 CPU 占用提高）。惰性删除则没有这个延迟：一旦
被访问，立刻删除。

**过期 key 的传播语义**

Redis 复制体系中，**只有主节点（master）负责主动删除过期 key，并把 DEL 命令传
播给副本（replica）和 AOF**。副本不主动做过期检查，只等待 master 发来的 DEL。
理由：

- 如果副本自行决定过期时间，时钟漂移会导致 master 与 replica 数据不一致。
- 强制"主节点删除 → DEL 广播"保证了**因果一致性**：replica 删除的时刻与
  master 完全同步。

hayakv 在主动过期路径（`expireIfNeeded`）里也遵循了这条规则：删除 key 后立刻
调用 `db.addAof(toExpireDelAof(key))`，向 AOF / replica 传播一条明确的 `DEL`。

### 2.2 maxmemory 与八种淘汰策略

当 Redis 占用的内存超过 `maxmemory` 配置时，在执行新写命令前会先触发**内存淘汰
（eviction）**。淘汰策略由 `maxmemory-policy` 控制，共八种，分两个维度：

**作用域**（对谁淘汰）：

- `allkeys-*`：从**所有** key 中挑选牺牲者
- `volatile-*`：只从**带 TTL** 的 key 中挑选，持久 key 绝不碰

**选择算法**：

| 后缀 | 算法 | 适用场景 |
|---|---|---|
| `-lru` | 淘汰最久未访问的 key（近似 LRU） | 通用缓存，热点访问分布明显 |
| `-lfu` | 淘汰访问频率最低的 key（近似 LFU） | 访问频率分布长尾，LRU 会误淘热点 |
| `-random` | 随机淘汰 | 访问分布均匀，省 CPU |
| `-ttl` | 淘汰最快过期的 key（`volatile-ttl` 专用） | 希望内存紧张时优先清理短命 key |
| `noeviction` | 拒绝新写命令，返回 `-OOM` 错误 | 数据库模式，宁可报错也不丢数据 |

**近似 LRU 的权衡**

精确 LRU 需要维护一条全局双向链表，每次访问都要把节点移到链表头部。在百万
key 的规模下，这条链表本身就要消耗数十 MB，且并发写链表的锁竞争严重。

Redis 的做法是**近似 LRU**：不维护任何链表，只在每个 key 的 `robj.lru` 字段里
存一个 24 位的粗粒度时间戳（精度 1 秒）。淘汰时从键空间随机采样
`maxmemory-samples`（默认 5）个 key，选其中 `lru` 值最旧的那个驱逐。

内存节省是量级级别的：精确 LRU 每个 key 需要两根指针（16 字节）+链表节点，
近似 LRU 只占 3 字节（24 位打包进 robj）。精度损失在大多数缓存场景下可以忽略。

**近似 LFU 的设计**

LFU 比 LRU 更难近似，因为计数器会无限增长，且旧的访问记录会"污染"频率
（一个一小时前被疯狂访问、现在彻底冷下来的 key，计数器仍然很高）。

Redis 用了两个技巧解决这两个问题：

1. **对数递增计数器（8 位）**：访问时以概率 `1/((counter-5)*factor+1)` 递增计
   数器，counter 越高增速越慢，最终饱和在 255。这让 8 位就能区分"几次"到
   "百万次"的访问量级差异。
2. **时间衰减**：将最后访问的分钟数（16 位）打包在同一个 uint32 里；淘汰前先
   计算时间衰减（每过 `lfu-decay-time` 分钟 counter 减 1），让不再热的 key
   自然降温。

### 2.3 小结

```
客户端 GET k
    └─ GetEntity(k)
           └─ IsExpired(k)? ──是──▶ Remove(k) + 传播 DEL ──▶ 返回 nil
                  │否
                  ▼
              touchLRU(k)   ← 更新 lruMap（LRU 时钟 或 LFU 计数器）
                  │
                  ▼
              返回 entity

写命令 SET k v
    └─ isDenyOOM? ──是──▶ freeMemoryIfNeeded()
                              └─ 超限? ──是──▶ evictOneKey()…循环
                                         └─ 仍超限(noeviction)? ──▶ 返回 -OOM

后台 StartCron（每 1/hz 秒）
    └─ activeExpireAllDBs()
           └─ 每个 DB: activeExpireCycle(sampleSize=20, maxLoops=16)
```

---

## ③ hayakv 实现带读

### 3.1 主动采样循环：`expire_cycle.go`

```go
// expire_cycle.go:19-20
const (
    activeExpireKeysPerLoop     = 20 // Redis ACTIVE_EXPIRE_CYCLE_KEYS_PER_LOOP
    activeExpireAcceptableStale = 25 // percent: keep looping while >25% sampled keys were expired
)
```

参数与真实 Redis 完全一致：每轮 20 个，25% 阈值。

核心循环（`expire_cycle.go:28–58`）：

```go
func (db *DB) activeExpireCycle(cfg activeExpireConfig) int {
    // cfg.sampleSize 默认 20，cfg.maxLoops 默认 16
    for loop := 0; loop < cfg.maxLoops; loop++ {
        sample := db.ttlMap.RandomDistinctKeys(cfg.sampleSize)
        expired := 0
        for _, key := range sample {
            if db.expireIfNeeded(key) {
                expired++
            }
        }
        // 停止条件：过期比例 ≤ 25%
        if expired*100/len(sample) <= activeExpireAcceptableStale {
            break
        }
    }
    ...
}
```

注意 hayakv 加了一个**上限 `maxLoops=16`**（真实 Redis 是时间预算，而不是循
环次数）。这是一个有意的简化：hayakv 使用 goroutine-per-connection 网络模型，
后台 cron goroutine 阻塞时不影响主命令路径；但加上 maxLoops 上限可以防止极端
场景下 cron 无限自旋。

**调度路径**（`server.go:671–705`）：

```go
func (server *Server) StartCron() {
    server.serverCronDone = make(chan struct{})
    go func(mdb *Server, done <-chan struct{}) {
        ticker := time.NewTicker(serverCronPeriod())  // 1/hz
        for {
            select {
            case <-ticker.C:
                mdb.activeExpireAllDBs()
            case <-done:
                return
            }
        }
    }(server, server.serverCronDone)
}
```

`serverCronPeriod()` 读取 `config.Properties.Hz`（默认 10），返回
`time.Second / 10 = 100ms`。整个循环跑在**独立的 goroutine** 里，与 05 章的
eventloop 完全解耦——`activeExpireAllDBs` 与 eventloop 的 `expireBlockedClients`
（处理 BLPOP 等阻塞命令的超时）是两个独立机制，互不依赖。

每次 ticker 触发：对每个 DB 依次调用 `activeExpireCycle` + `activeHashFieldExpireCycle`（处理 Hash field 级别的 TTL，Redis 8 新增特性）。

**`expireIfNeeded`：双重检查（check-lock-check）**（`expire_cycle.go:63–88`）：

```go
func (db *DB) expireIfNeeded(key string) bool {
    rawTTL, ok := db.ttlMap.Get(key)   // ① 无锁快检
    if !ok { return false }
    expireTime, _ := rawTTL.(time.Time)
    if !time.Now().After(expireTime) { return false }

    db.RWLocks([]string{key}, nil)     // ② 拿写锁
    defer db.RWUnLocks([]string{key}, nil)

    rawTTL, ok = db.ttlMap.Get(key)   // ③ 锁内再检（TTL 可能已被 PERSIST 清除）
    if !ok { return false }
    expireTime, _ = rawTTL.(time.Time)
    if !time.Now().After(expireTime) { return false }

    db.Remove(key)
    db.addAof(toExpireDelAof(key))     // ④ 传播 DEL 给 AOF/replica
    return true
}
```

这个 check-lock-check 模式防止了竞态：在①和②之间，另一个 goroutine 可能已经
用 `PERSIST` 清除了 TTL，或者用 `EXPIRE` 重置了到期时间。锁内二次检查确保不会
错误删除一个已经被续命的 key。

### 3.2 惰性删除检查点：`GetEntity`

惰性删除的检查点在 `database.go:295`：

```go
func (db *DB) GetEntity(key string) (*database.DataEntity, bool) {
    raw, ok := db.data.GetWithLock(key)
    if !ok { return nil, false }
    if db.IsExpired(key) {   // ← 惰性删除检查
        return nil, false
    }
    entity, _ := raw.(*database.DataEntity)
    db.touchLRU(key)         // ← LRU/LFU 访问元数据更新
    return entity, true
}
```

所有读命令（`GET`、`HGET`、`LRANGE` 等）都通过 `GetEntity` 读取 key，因此惰性
删除对所有命令生效，无需在每个命令handler里重复实现。

注意 `touchLRU` 也在这里调用——**每次成功读取 key 都会更新 LRU 时钟或 LFU 计
数器**，这保证了访问热度的实时追踪。

### 3.3 `evict.go`：淘汰策略与 LRU/LFU 存储

**LRU/LFU 元数据存在哪里？**

hayakv 的 value 层使用原生 Go 类型（`[]byte`、`*object.Robj` 等），没有像 Redis
的 `robj` 那样的通用头部字段。为了不侵入 value 层，hayakv 用**一张独立的
`lruMap`（`ConcurrentDict`）** 存储每个 key 的访问元数据：

```go
// evict.go:93-96
type lruMeta struct {
    data uint32
}
// LRU 模式：低 24 位存时钟值
// LFU 模式：高 16 位存分钟数，低 8 位存对数计数器
```

一个 `uint32` 字段复用两种布局，与 Redis `robj.lru` 的设计完全对应。

**LRU 时钟**：精度 1000 ms，24 位，最大值 `(1<<24)-1 = 16777215`，约 194 天后
溢出归零。`lruIdleFor` 正确处理了 24 位回绕：

```go
// evict.go:59-63
func lruIdleFor(now, access uint32) uint32 {
    if now >= access { return now - access }
    return (lruClockMax - access) + now + 1  // 处理回绕
}
```

**LFU 对数计数器**（`evict.go:69-83`）：

```go
func lfuLogIncr(counter uint8) uint8 {
    if counter == 255 { return 255 }
    baseval := float64(counter) - lfuInitVal  // lfuInitVal=5（新 key 初始值）
    p := 1.0 / (baseval*lfuLogFactor + 1)    // lfuLogFactor=10
    if rand.Float64() < p { return counter + 1 }
    return counter
}
```

新 key 的初始计数器是 5（`lfuInitVal`），理由是：新 key 刚被写入就很可能马上
被读（局部性原理），从 5 起步避免被立即当作冷 key 淘汰。

**时间衰减**（`evict.go:139-152`）：每经过 `decayMinutes=1` 分钟，计数器减 1。
防止"历史热点"永久占据高位。

**何时触发淘汰？**（`database.go:178-186`）

每条带 `denyoom` 标志的**写命令**（如 `SET`、`HSET`、`LPUSH` 等）执行前，在全
局 `memMu` 锁保护下先调用 `freeMemoryIfNeeded()`：

```go
if isDenyOOMCmd {
    db.server.memMu.Lock()
    defer db.server.memMu.Unlock()
    if !db.server.freeMemoryIfNeeded() {
        return oomErrReply()  // -OOM command not allowed when used memory > 'maxmemory'.
    }
    ...
}
```

`freeMemoryIfNeeded` 循环调用 `evictOneKeyWithSize`，**增量扣减**估算的释放字节
数，而不是每次都重新调用 `usedMemory()`（后者是 O(N) 全表遍历），避免了淘汰循
环的性能悬崖。

**used memory 怎么算？**

Redis 用 jemalloc 的 `zmalloc_used_memory()` 直接拿真实分配字节数。Go 没有这个
接口，hayakv 改用**确定性静态估算**（`memory.go:27-34`）：

```go
const (
    perKeyOverhead  = 56  // dictEntry + key sds 头近似
    perElemOverhead = 16  // 集合元素的 bookkeeping
    robjOverhead    = 16  // robj 头部
)

func estimateEntitySize(key string, entity *database.DataEntity) int64 {
    size := int64(perKeyOverhead + len(key))
    size += valueSize(entity.Data)  // 递归按类型估算 value
    return size
}
```

`usedMemory()（server.go:738）`遍历所有数据库的所有 key，将 `estimateEntitySize`
累加。这是 O(N) 操作；hayakv 通过在淘汰循环里**增量扣减**来减少调用次数，而不
是在每次驱逐后重新全量统计。

注意这是**有意的确定性估算**，而非精确字节数——注释里明确写道"deliberate,
deterministic estimates (NOT real Redis jemalloc sizes) so the accounting is
reproducible for tests"。

### 3.4 差分语料佐证：`corpus_expiry_test.go`

`test/diff/corpus_expiry_test.go` 里的语料精心区分了**可差分**和**不可差分**的
行为：

**可差分**（确定性，逐字节对拍）：

- `EXPIRE k 100` + `TTL k` → 精确整数 `100`
- `PERSIST k` + `TTL k` → `-1`（永久）
- `PEXPIREAT k 1`（已过期的过去时刻）+ `GET k` → 惰性删除 → `nil`
- `EXPIRETIME k` → 精确 Unix 秒数
- noeviction + `maxmemory 1` + `SET` → 精确 `-OOM` 错误字符串
- `CONFIG GET maxmemory-policy` 往返

**不可差分**（被排除）：

- `used_memory`、`INFO memory`——Go 估算 vs Redis jemalloc 数值不同
- 近似 LRU/LFU 淘汰**选择哪个** key——采样随机，两边不会一致

"固定 TTL 语义检查"的核心场景是 `pexpireat in the past deletes`：给 key 设置一
个 epoch=1ms 的过去时刻，Redis 规范要求它**立刻**被标记过期并在下一次访问时惰
性删除。这个场景既测试了过期时间的存储精度，也测试了惰性删除的触发时机。

---

## ④ 动手验证

> 所有命令在仓库根目录执行，需先 `go run ./cmd/hayakv`（监听 :6399）。

**实验 1：PX 毫秒过期**

```bash
$ redis-cli -p 6399 set k_px v px 100
OK
$ sleep 0.25
$ redis-cli -p 6399 get k_px
(nil)
```

key 在 100 ms 后惰性删除，`GET` 返回 `(nil)`。

**实验 2：EX 秒过期 + TTL 查询**

```bash
$ redis-cli -p 6399 set k2 v2 ex 100
OK
$ redis-cli -p 6399 ttl k2
(integer) 100
```

**实验 3：PERSIST 取消 TTL**

```bash
$ redis-cli -p 6399 persist k2
(integer) 1
$ redis-cli -p 6399 ttl k2
(integer) -1
```

`-1` 表示 key 存在但无 TTL（永久）。

**实验 4：CONFIG SET maxmemory-policy**

```bash
$ redis-cli -p 6399 config set maxmemory-policy allkeys-lru
OK
$ redis-cli -p 6399 config get maxmemory-policy
1) "maxmemory-policy"
2) "allkeys-lru"
$ redis-cli -p 6399 config set maxmemory-policy noeviction
OK
```

`maxmemory-policy` 是运行时可改的（CONFIG SET），无需重启。

**实验 5：单元测试**

```bash
$ go test -race ./internal/command \
    -run "TestActiveExpire|TestEviction|TestPolicy|TestLRU|TestLFU|TestExpire" \
    -count=1 -v 2>&1 | tail -25
=== RUN   TestPolicyNoevictionReturnsError
--- PASS: TestPolicyNoevictionReturnsError (0.54s)
=== RUN   TestPolicyAllkeysRandomEvictsSomething
--- PASS: TestPolicyAllkeysRandomEvictsSomething (0.94s)
=== RUN   TestPolicyVolatileOnlyTouchesTTLKeys
--- PASS: TestPolicyVolatileOnlyTouchesTTLKeys (0.60s)
=== RUN   TestEvictionCandidatePicksColdestLRU
--- PASS: TestEvictionCandidatePicksColdestLRU (0.28s)
=== RUN   TestLRUClockMonotonicAndMasked
--- PASS: TestLRUClockMonotonicAndMasked (0.00s)
=== RUN   TestLFULogIncrSaturates
--- PASS: TestLFULogIncrSaturates (0.02s)
=== RUN   TestActiveExpireReapsWithoutRead
--- PASS: TestActiveExpireReapsWithoutRead (0.02s)
=== RUN   TestActiveExpireSkipsLiveKeys
--- PASS: TestActiveExpireSkipsLiveKeys (0.01s)
PASS
ok  	github.com/amemiya02/hayakv/internal/command	9.277s
```

所有测试通过（含 `-race`，检测到并发安全）。

**实验 6：差分测试**

```bash
$ go test ./test/diff -run TestDifferentialExpiry -count=1 -v
--- SKIP: TestDifferentialExpiry (1.43s)
PASS
```

本机无 Docker / 真实 Redis 实例，测试自动跳过（SKIP），不影响 CI。
提供 `HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379` 环境变量后可完整运行。

---

## ⑤ 延伸阅读

- **Redis 官方文档**
  - [Key expiration](https://redis.io/docs/latest/commands/expire/)：EXPIRE
    命令及过期语义完整说明
  - [Using Redis as an LRU cache](https://redis.io/docs/latest/develop/reference/eviction/)：
    maxmemory 策略详解与近似 LRU 的原理说明，包含精确 LRU vs 近似 LRU 的命中
    率对比图
- **Redis 源码**（GitHub `redis/redis`）
  - `expire.c`：惰性删除（`expireIfNeeded`）与主动过期循环
    （`activeExpireCycle`）的 C 实现
  - `evict.c`：八种策略、近似 LRU 采样池（`evictionPoolPopulate`）、LFU 对数
    计数器（`LFULogIncr`）
- **本项目配置参考**：[`../dev/config.md`](../dev/config.md)
  — `hz`、`maxmemory`、`maxmemory-policy`、`maxmemory-samples` 的默认值与
  运行时可改性说明
- **第 07 章预告**：[RDB、multi-part AOF 与混合持久化](07-persistence.md)
  — 本章提到的 `addAof(toExpireDelAof(key))` 会写进哪个文件？AOF 的
  rewrite 和 RDB 快照如何处理带 TTL 的 key？下一章揭晓。
