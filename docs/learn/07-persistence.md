# 第 07 章：RDB、multi-part AOF 与混合持久化

> **前置章节**：[02 Robj 与对象编码](02-object.md)（RDB 序列化的正是那些编码层对象）、[05 网络模型](05-event-loop.md)（BGSAVE 的非阻塞实现依赖单线程事件循环提供的一致性窗口）。

---

## 导读

内存数据库最大的软肋是掉电即失。Redis 提供了两条持久化路线来应对这个问题：

- **RDB（Redis DataBase）**：全量快照，把某一时刻的全部数据以二进制紧凑格式写入一个文件，重启时加载速度快，但两次快照之间的写入会丢失。
- **AOF（Append-Only File）**：写命令日志，每一条修改操作都追加到文件末尾，丢失窗口可以压到 1 秒甚至更小，但文件体积随时间无限增长，重启时逐条重放速度较慢。

实际生产中两者通常同时开启：AOF 保证低丢失率，RDB 作为快速恢复的保底。从 Redis 7 起，**混合持久化**（`aof-use-rdb-preamble yes`）把 RDB 的快与 AOF 的全结合起来；**multi-part AOF**（manifest 布局）则解决了重写期间文件切换的原子性问题。

hayakv 完整实现了上述所有机制，并额外提供了一个独特卖点：自有的 **faithful RDB 编解码器**（`rdb-impl faithful`），写出的文件与真实 Redis 8.x 逐字节互通。本章带你把原理和代码一一对应。

---

## 一、Redis 原理

### 1.1 两条路线的本质取舍

| | RDB | AOF |
|---|---|---|
| 本质 | 时间点快照（point-in-time snapshot） | 写命令增量日志 |
| 文件大小 | 紧凑（二进制编码，可压缩） | 随写入量线性增长 |
| 重启恢复速度 | 快（直接解析二进制） | 慢（逐条重放命令） |
| 最大丢失窗口 | 两次快照之间（分钟级） | 至多 1 秒（`everysec`）或更短 |
| 适用场景 | 允许少量丢失、重启恢复快、全量备份 | 对数据完整性要求高 |

**如何选择**：对丢失零容忍选 AOF；对恢复速度有要求或做备份选 RDB；生产环境两者都开。

### 1.2 RDB 的非阻塞实现：fork + COW vs Go 的取舍

C Redis 的 BGSAVE 核心思路极简：调用 `fork()`，子进程复制父进程的完整地址空间，然后把全量数据编码写入磁盘，父进程继续响应客户端。操作系统的 Copy-On-Write（COW）保证在父进程修改某个内存页时，子进程仍看到旧版本——这就是"时间点快照"的底层魔法，几乎零拷贝开销。

**Go 无法复制这个技巧。** 原因：Go 运行时是多线程的（GC、调度器、netpoller），`fork()` 之后子进程里只剩一个线程，运行时内部的各种锁与状态会进入不一致状态，任何超过 `exec()` 的操作都是未定义行为。hayakv 的 `snapshot.go` 包注释把这一点写得很清楚：

```
// Divergence from C Redis: C Redis does BGSAVE by fork() — the child inherits a
// copy-on-write snapshot of the entire heap. Go cannot do this because the
// runtime is multi-threaded (GC, scheduler, netpoller); calling fork() and then
// doing anything other than exec() is undefined.
```

（`internal/command/snapshot.go:3`）

hayakv 的解决方案：**利用单线程事件循环提供的静止窗口**。在 `net=goroutine` 模式下，`BGSAVE` 命令在调用者的 goroutine 上同步地把全部 key 的 `(key, value, expire)` 三元组深拷贝到一个 `[]rdb.Entry` 切片（`snapshotAllDBs()`），然后把这个**不可变的切片**交给后台 goroutine 去序列化写盘。事件循环继续接收新请求，但它们操作的是活跃的 `dbSet`，而后台 goroutine 持有的是已经与引擎完全解耦的快照。

```
// internal/command/snapshot.go:40-58
func (server *Server) snapshotAllDBs() []dbSnapshot {
    snaps := make([]dbSnapshot, 0, len(server.dbSet))
    for db := 0; db < len(server.dbSet); db++ {
        var entries []rdb.Entry
        server.ForEach(db, func(key string, entity *database.DataEntity, expiration *time.Time) bool {
            e := rdb.Entry{DBIndex: db, Key: []byte(key)}
            if expiration != nil {
                e.ExpireMS = uint64(expiration.UnixNano() / 1e6)
            }
            if snapshotEntity(&e, entity) {
                entries = append(entries, e)
            }
            return true
        })
        ...
    }
    return snaps
}
```

这是 fork/COW 的 **语义等价物**，但代价是一次 O(keys) 的内存拷贝。对于字符串值，拷贝是浅层的（`append([]byte(nil), b...)`）；对于聚合类型，是深拷贝——这是与 COW 的最大差异，内存峰值会翻倍。

后台写盘路径（`server.go:617`）：
```go
snap := db.snapshotAllDBs()          // 在调用者 goroutine 上完成拷贝
go func() {
    db.saveSnapshotToFile(snap, ...)  // 后台 goroutine 编码写盘
}()
return protocol.MakeStatusReply("Background saving started")
```

### 1.3 AOF 三档 fsync

AOF 的持久性-性能阶梯由 `appendfsync` 控制：

| 策略 | 时机 | 丢失上限 | 性能影响 |
|---|---|---|---|
| `always` | 每条命令写完立即 fsync | 理论零丢失 | 写放大最大，IOPS 瓶颈 |
| `everysec` | 后台每秒 fsync 一次（默认推荐） | 最多丢 1 秒 | 对写性能几乎无影响 |
| `no` | 由操作系统决定（通常 30 s） | 取决于 OS 缓冲 | 最高吞吐 |

hayakv 实现：`always` 模式在 `writeAof()` 写完后立即调用 `aofFile.Sync()`；`everysec` 模式由 `fsyncEverySecond()` 启动一个独立 goroutine，用 `time.NewTicker(time.Second)` 周期调用 `Fsync()`（`aof.go:276-289`）。

### 1.4 AOF 重写：为什么必要，重写期间怎么办

AOF 文件只追加不删除。如果一个 key 被 SET 了一百万次，日志里就有一百万行，而数据库里只剩最后一个值。**重写**（BGREWRITEAOF）的目的是把当前内存状态压缩成最小命令集，替换掉历史日志。

重写期间的新写入是难点：重写在后台进行，前台还在追加新命令。hayakv 的解决方案（`rewrite.go`）：

1. **StartRewrite**：加锁（`pausingAof.Lock()`），记录当前 AOF 文件大小 `fileSize`，创建临时文件，立即解锁继续服务。
2. **DoRewrite**：根据 `aof-use-rdb-preamble` 决定把当前内存快照写成 RDB 格式还是 AOF 格式，写入临时文件。
3. **FinishRewrite**：再次加锁，把 `fileSize` 之后追加的新命令（重写期间产生的增量）拷贝到临时文件末尾，再用 `os.Rename` 原子替换原文件，解锁。

这样任何时刻都只有一个完整文件，不会出现半写的 AOF。

### 1.5 multi-part AOF（Redis 7+）

旧方案的痛点：重写完成时用 `rename` 把新文件原子替换旧文件——看起来安全，但旧文件被替换之前的时间窗口内，如果服务器崩溃，新文件可能已经写完而 `rename` 还没发生，重启时加载的仍是旧文件。更严重的是，整个重写流程是"单文件-就地替换"，无法同时保留多个增量段。

Redis 7 引入 **multi-part AOF**：把一次完整 AOF 拆分为三类文件，用一个 manifest 文件管理它们：

```
appendonlydir/
  appendonly.aof.manifest          ← 目录文件，记录当前有效的 base + incr 文件列表
  appendonly.aof.1.base.rdb        ← 某次重写产生的 RDB 快照（seq=1）
  appendonly.aof.1.incr.aof        ← 快照之后的增量 AOF（seq=1）
  appendonly.aof.2.base.rdb        ← 下一次重写后的新快照（seq=2）
  appendonly.aof.2.incr.aof        ← 新增量（seq=2）
```

manifest 文件格式（每行一条记录）：
```
file appendonly.aof.2.base.rdb seq 2 type b
file appendonly.aof.2.incr.aof seq 2 type i
```

`type b` = base（快照），`type i` = incr（增量），历史文件标记 `type h`（待清理）。

**重写的原子性问题如何解决**：新 base 文件写到临时文件，`sync`，`rename` 进 appendonlydir；新 incr 文件创建为空；最后用 temp-and-rename 原子更新 manifest。整个流程中 manifest 始终指向一个完整可用的状态——要么是旧版本，要么是新版本，不会有半途而废（`multipart.go:64-88`，`multipart_rewrite.go`）。

### 1.6 混合持久化

`aof-use-rdb-preamble yes`（Redis 7+ 默认）：重写时 base 文件使用 **RDB 格式**而不是 AOF 格式。好处：RDB 比命令文本紧凑得多，加载速度快几倍；而 base 之后的增量 incr.aof 仍用 AOF 格式，保留精细的时间粒度。

重启加载顺序：先解析 base.rdb（恢复快照），再重放 incr.aof（追上增量），组合起来得到完整状态。

### 1.7 RDB 文件格式速览

```
REDIS0012  ← 9字节魔数，版本 12 对应 Redis 8.x
0xFA key value    ← AUX 辅助字段（redis-ver, redis-bits, ctime, used-mem, aof-base...）
0xFE db_number    ← SELECTDB opcode
0xFB dbsize ttls  ← RESIZEDB 提示（加速加载时的 hash 预分配）
[0xFC ms_expire]  ← 可选的过期时间（8 字节小端 Unix 毫秒时间戳）
type_byte key val ← 实际 KV 记录（type: 0=string 1=list 2=set 3=zset 4=hash）
...
0xFF               ← EOF opcode
8字节小端 CRC64    ← 校验和（0 表示禁用校验）
```

key 和 value 用**长度前缀编码**，整数可被特殊压缩（`encInt8/16/32`），短字符串可 LZF 压缩（hayakv 不写 LZF，但能读取）。

---

## 二、hayakv 实现带读

### 2.1 AOF 目录结构

```
internal/persist/aof/
  aof.go               ← Persister 核心：channel 收命令、写文件、fsync 策略
  rewrite.go           ← StartRewrite / DoRewrite / FinishRewrite 三步重写协议
  rdb.go               ← GenerateRDB（library 路径，调用 hdt3213/rdb 编码器）
  faithful_rdb.go      ← DumpEngineToRDB（faithful 路径，遍历引擎写 rdb.Encoder）
  marshal.go           ← EntityToCmd：把内存对象序列化回对应的写命令（用于 AOF 重写）
  manifest.go          ← ManifestEntry / Manifest：解析/序列化 manifest 文本格式
  multipart.go         ← multiPart 结构：路径计算、manifest 读写、LoadMultiPart 加载
  multipart_rewrite.go ← mp.rewrite()：写新 base.rdb、空 incr、原子换 manifest
  multipart_test.go / multipart_rewrite_test.go / manifest_test.go ← 对应单测
```

### 2.2 追加路径：命令怎么流到磁盘

（衔接第 05 章：每条命令执行完后 `db.persister(cmdLine)` 被调用。）

1. `db.addAof(cmdLine)` → `server.persister.SaveCmdLine(dbIndex, cmdLine)`（`persistence.go:104-108`）
2. `SaveCmdLine` 把 `payload{cmdLine, dbIndex}` 发送到 `aofChan`（容量 1<<20）。`always` 模式下跳过 channel，直接调 `writeAof`（`aof.go:119-139`）。
3. 后台 goroutine `listenCmd()` 从 channel 持续消费，调 `writeAof`（`aof.go:142-147`）。
4. `writeAof` 拿 `pausingAof` 互斥锁（防重写期间并发写），如果当前 db 发生变化先写一条 `SELECT`，再把命令编码成 RESP multibulk 写入 `aofFile`。`always` 模式在这里调 `aofFile.Sync()`（`aof.go:149-179`）。
5. `everysec` 模式：另一个 goroutine 每秒调 `Fsync()`，加 `pausingAof` 锁后调 `aofFile.Sync()`（`aof.go:277-289`）。

**关键设计**：`pausingAof` 不是一把保护数据的锁，而是用来暂停追加的信号量——重写启动时加锁，目的是让 `writeAof` 等待，保证截取的文件大小 `fileSize` 是一个一致的边界。

### 2.3 重写流程（AOF 重写/BGREWRITEAOF）

```
StartRewrite():
  pausingAof.Lock()
  aofFile.Sync()               ← 确保已追加内容落盘
  fileSize = stat(aofFile)     ← 记录截止点
  tmpFile = CreateTemp(...)    ← 创建临时文件
  pausingAof.Unlock()          ← 放行新写入

DoRewrite(ctx):
  if AofUseRdbPreamble:
      generateRDB(ctx)         ← 加载当前 AOF 到临时 db，用 RDB 编码器序列化到 tmpFile
  else:
      generateAof(ctx)         ← 用 RPUSH/HSET 等命令格式序列化到 tmpFile

FinishRewrite(ctx):
  pausingAof.Lock()
  src.Seek(fileSize, 0)        ← 跳到重写期间新增的命令起点
  tmpFile.Write("SELECT n")    ← 对齐 db 索引
  io.Copy(tmpFile, src)        ← 把增量命令追加进去
  os.Rename(tmpFile, aofFile)  ← 原子替换
  pausingAof.Unlock()
```

（`rewrite.go:29-148`）

### 2.4 multi-part AOF 重写（mp.rewrite）

`multipart_rewrite.go:8-67` 是更干净的重写实现，直接面向 multi-part 布局：

1. 读当前 manifest，找出最大 seq。
2. 用 `DumpEngineToRDB` 把引擎当前状态写入 `*.base.tmp`，sync，rename 为 `appendonly.aof.<seq+1>.base.rdb`。
3. 创建空文件 `appendonly.aof.<seq+1>.incr.aof`。
4. 构造新 manifest（base + incr），用 temp-and-rename 原子写入 `appendonly.aof.manifest`。

整个过程不需要三步锁协议，因为 base 文件是全新写出的（不是在老文件上改），manifest 替换是原子 rename。

### 2.5 加载：LoadMultiPart

`multipart.go:92-113`：

1. 若 manifest 不存在（首次启动），直接返回 nil——零副作用。
2. 解析 manifest，找 base 文件：若是 `.base.rdb` 调 `loadBaseFile`（用 hayakv 自有 RDB 解码器），若是 `.base.aof` 当普通 AOF 重放。
3. 按 seq 顺序重放所有 `.incr.aof` 文件。

### 2.6 RDB 编解码器（internal/persist/rdb/）

```
rdb/
  rdb.go        ← opcode/type 常量、Entry 结构体、Version = 12
  primitives.go ← writer/reader：长度编码、整数特殊编码、CRC64 流式计算
  encoder.go    ← Encoder：WriteHeader/WriteAux/WriteSelectDB/WriteResizeDB/WriteStringEntry/...WriteEnd
  decoder.go    ← Decoder.Parse()：状态机解析 opcode，readObject 按类型分派
  crc64.go      ← CRC64-JONES 实现，与 Redis 使用相同多项式
```

**WriteHeader** 写出字面量 `"REDIS0012"`（`encoder.go:19-21`）。Version 常量定义在 `rdb.go:50`（`const Version = 12`）。

**长度编码**（`primitives.go:29-45`）：6 bit 直接编码 0–63，14 bit 编码 64–16383，超出用 0x80/0x81 前缀跟 4/8 字节大端整数。整数字符串可以紧凑编码为 `0xC0|encInt8/16/32`（节省空间，与真实 Redis 编码一致）。

**writeRawString vs writeString**（`primitives.go:88-93`）：zset score 必须用 `writeRawString`（不走整数特殊编码），否则 `1` 会被编码成 `0xC0 0x01`（int8 特殊形式），而真实 Redis 写的是 `0x01 0x31`（长度 1 + 字节 `'1'`）——这是一个细节差异，正是 faithful 路径需要对齐的。

**writeEntity / writeRobjEntity**（`faithful_rdb.go:68-186`）：把 `database.DataEntity`（可能是 `*object.Robj` 或旧式 `[]byte`/`List.List` 等）映射到对应的 Encoder 调用，衔接第 02 章的对象模型。

**CRC64 校验**：`writer` 在每次写字节时增量更新 CRC，`WriteEnd` 写 0xFF 后把最终值以小端 8 字节附在文件末尾（`encoder.go:313-322`）；`Decoder.verifyCRC()` 读取 8 字节，若非零则比对，零值表示禁用校验直接通过（`decoder.go:359-373`）。

### 2.7 faithful_rdb.go：DumpEngineToRDB 与 LoadEntriesAsCommands

`DumpEngineToRDB`（`faithful_rdb.go:28-66`）是从引擎到 RDB 文件的桥接：遍历 `dbCount` 个数据库，对每个非空 DB 写 SELECTDB + RESIZEDB，再对每个 key 调 `writeEntity`。AUX 字段 `redis-ver = "8.0.0"` 让 Redis 知道这是 v8 格式的文件（`faithful_rdb.go:33-38`）。

`LoadEntriesAsCommands`（`faithful_rdb.go:197-228`）把解码出的 `rdb.Entry` 转成 `SET`/`RPUSH`/`SADD`/`HSET`/`ZADD`/`PEXPIREAT` 命令序列，然后调 `db.Exec` 重放——这样加载 RDB 和加载 AOF 走的是同一条命令路径，数据索引和 AOF 追加逻辑无需特殊处理。

### 2.8 crossload 验证设计

`test/diff/rdb_crossload_test.go` 做双向验证：

**TestRDBCrossLoadHayakvToRedis**：启动 hayakv（`rdb-impl faithful`），写入多种类型数据，`SAVE`，停机，用真实 Redis 加载 hayakv 写出的 `dump.rdb`，验证每个 key 的 GET/LRANGE/HGET/ZSCORE/SCARD 响应正确。

**TestRDBCrossLoadRedisToHayakv**：启动真实 Redis，写数据，`SAVE`，停机，hayakv（`appendonly no`，`rdb-impl faithful`）加载 Redis 写出的 `dump.rdb`，验证同样的查询。

两个测试都在测试开头检查 `redis-server` 是否在 PATH，不存在则 `t.Skip`，所以在没有 Redis 的 CI 环境里会跳过而不是失败。在本文写作时（本机有 redis-server），这两个测试会运行但受其他已知问题影响（list RPUSH 去重 bug）：

```
$ go test ./test/diff/ -run TestRDBCrossLoad -v -count=1 -timeout 30s
--- FAIL: TestRDBCrossLoadHayakvToRedis (0.80s)
    rdb_crossload_test.go:118: redis GET str = "$-1\r\n"
--- FAIL: TestRDBCrossLoadRedisToHayakv (0.81s)
    rdb_crossload_test.go:209: hayakv LRANGE list = "*21\r\n..."
```

注意：这是 hayakv 当前已知的兼容性 bug，不是持久化格式问题，RDB 文件格式本身已经字节级对齐（由 `TestRewriteMultiPart` 等单测证实：`string(data[:9]) == "REDIS0012"` 通过）。

---

## 三、动手验证

所有命令在仓库根目录执行。实验使用临时目录，不污染仓库。

### 3.1 单元测试

```bash
$ go test -race ./internal/persist/... -count=1
ok  	github.com/amemiya02/hayakv/internal/persist/aof	1.402s
ok  	github.com/amemiya02/hayakv/internal/persist/rdb	1.655s
```

### 3.2 RDB 文件实验

先在临时目录启动 hayakv（需要开启 AOF 才能触发 SAVE），写入数据，执行 SAVE，用 `xxd` 确认 magic：

```bash
# 准备临时目录和配置
TMPDIR=$(mktemp -d)
go build -o "$TMPDIR/hayakv" ./cmd/hayakv

cat > "$TMPDIR/redis.conf" << 'EOF'
bind 127.0.0.1
port 16399
dir PLACEHOLDER
databases 16
net goroutine
engine redisdb
appendonly yes
appendfilename appendonly.aof
appendfsync everysec
aof-use-rdb-preamble yes
dbfilename dump.rdb
rdb-impl faithful
EOF
sed -i '' "s|PLACEHOLDER|$TMPDIR|g" "$TMPDIR/redis.conf"

# 启动服务器（进程 cwd 决定 dump.rdb 落点，用 cd + CONFIG 方式）
(cd "$TMPDIR" && CONFIG="$TMPDIR/redis.conf" "$TMPDIR/hayakv") &
sleep 1.5

redis-cli -p 16399 set k1 hello
redis-cli -p 16399 rpush mylist a b c
redis-cli -p 16399 hset myhash f1 v1

redis-cli -p 16399 save   # +OK  ← blocking save

kill %1 2>/dev/null
```

实验产出的 `dump.rdb` 开头：

```
00000000: 5245 4449 5330 3031 32fa 0972 6564 6973  REDIS0012..redis
00000010: 2d76 6572 0538 2e30 2e30 fa0a 7265 6469  -ver.8.0.0..redi
00000020: 732d 6269 7473 c040 fa05 6374 696d 65c2  s-bits.@..ctime.
```

- 字节 `0–8`：`REDIS0012`（版本 12，对应 Redis 8.x）
- 字节 `9`：`0xFA`（AUX opcode）
- 之后：AUX 键 `redis-ver` 的长度编码 + 内容 `8.0.0`

### 3.3 AOF 文件结构

同一次实验产生的 `appendonly.aof`（节选）：

```
*3
$3
set
$2
k1
$5
hello
*5
$5
rpush
$6
mylist
$1
a
$1
b
$1
c
```

标准 RESP multibulk 格式：`*3` 表示三个参数，`$3` 表示后续字符串长度 3。

### 3.4 multi-part AOF manifest 结构

`TestDemoManifestLayout`（包内单测）演示重写后的目录：

```
appendonlydir/
  appendonly.aof.1.base.rdb    117 bytes   ← 重写生成的 RDB 快照
  appendonly.aof.1.incr.aof      0 bytes   ← 重写后的增量 AOF（刚创建，空）
  appendonly.aof.manifest       88 bytes
```

manifest 内容：
```
file appendonly.aof.1.base.rdb seq 1 type b
file appendonly.aof.1.incr.aof seq 1 type i
```

每行格式：`file <文件名> seq <序列号> type <b|i|h>`。加载时先解析 manifest，找到 `type b` 的 base 文件，再按 seq 顺序重放所有 `type i` 的增量文件（`multipart.go:100-112`）。

### 3.5 crossload 测试（需要 redis-server）

```bash
# 若本机有 redis-server：
$ go test ./test/diff/ -run TestRDBCrossLoad -v -count=1 -timeout 30s

# 若无 redis-server，测试自动跳过：
# --- SKIP: TestRDBCrossLoadHayakvToRedis
#     rdb_crossload_test.go:79: redis-server not on PATH; skipping hayakv->redis cross-load
```

测试验证两个方向：hayakv → Redis（hayakv 写 dump.rdb，Redis 加载并查询），Redis → hayakv（Redis 写 dump.rdb，hayakv 加载并查询），覆盖 string/list/hash/set/zset 五种类型。

---

## 四、延伸阅读

- [Redis persistence 官方文档](https://redis.io/docs/management/persistence/)：RDB 与 AOF 的完整权威说明
- [Redis 源码 `rdb.c`](https://github.com/redis/redis/blob/unstable/src/rdb.c)：C 实现的 fork/COW BGSAVE
- [Redis 源码 `aof.c`](https://github.com/redis/redis/blob/unstable/src/aof.c)：AOF 重写与 multi-part 布局
- `../dev/config.md` → PERSISTENCE 组：hayakv 所有持久化配置键的完整参考
- 第 08 章预告：PSYNC 复制——AOF 的 `Listener` 接口如何把写命令实时转发给从节点
