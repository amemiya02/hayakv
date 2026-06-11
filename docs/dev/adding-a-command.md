# 如何添加一个命令

> **目标读者**：想为 hayakv 贡献新命令的 Go 开发者。  
> **范例命令**：`SETNX`（string if not exists）——它短小精悍，把完整流程都走了一遍。

---

## 目录

1. [命令从哪进来](#1-命令从哪进来)
2. [handler 怎么写](#2-handler-怎么写)
3. [注册命令](#3-注册命令)
4. [差分语料是强制的](#4-差分语料是强制的)
5. [四层测试怎么跑](#5-四层测试怎么跑)
6. [提交前 Checklist](#6-提交前-checklist)

---

## 1. 命令从哪进来

所有命令都登记在 `internal/command/router.go` 的全局命令表里：

```go
// internal/command/router.go:9
var cmdTable = make(map[string]*command)
```

命令表中的每条记录是一个 `command` 结构体（`router.go:11-23`）。注册调用 `registerCommand`：

```go
// internal/command/router.go:43
func registerCommand(name string, executor ExecFunc, prepare PreFunc,
    rollback UndoFunc, arity int, flags int) *command
```

**六个参数逐一解释：**

| 参数 | 类型 | 含义 |
|---|---|---|
| `name` | `string` | 命令名（不区分大小写，内部转 lower） |
| `executor` | `ExecFunc` | 实际执行函数，签名 `func(db *DB, args [][]byte) redis.Reply`（`database.go:93`）。`args` **不含** 命令名本身，只有参数 |
| `prepare` | `PreFunc` | 在命令进入 `MULTI` 事务队列时调用，签名 `func(args [][]byte) (writeKeys, readKeys []string)`（`database.go:97`）。返回值供事务层获取锁和检测冲突 |
| `rollback` | `UndoFunc` | 事务回滚时调用，签名 `func(db *DB, args [][]byte) []CmdLine`（`database.go:104`）。在命令真正执行**之前**先生成撤销日志；若事务需要回滚，则回放这些日志还原状态 |
| `arity` | `int` | 命令参数总数（含命令名）。正数表示精确匹配；**负数表示至少 `-arity` 个**，例如 `MGET` 的 arity 为 `-2`，即至少需要 2 个词（命令名 + 至少一个 key） |
| `flags` | `int` | 读写属性。`flagWrite = 0`（`router.go:35`）；`flagReadOnly = 1 << iota`（`router.go:38`）。只读命令须显式传 `flagReadOnly`，写命令传 `flagWrite`（即 `0`） |

除了 `registerCommand`，还有 `registerSpecialCommand`（`router.go:58`），用于 `PUBLISH`、`SELECT`、`KEYS`、`FLUSHALL` 等需要跨 DB 或特殊调度的命令。普通单 DB 命令用 `registerCommand` 即可。

---

## 2. handler 怎么写

以 `execSetNX` 为例（`internal/command/string.go:310-319`）：

```go
// execSetNX sets string if not exists
func execSetNX(db *DB, args [][]byte) redis.Reply {
    key := string(args[0])
    value := args[1]
    entity := &database.DataEntity{
        Data: object.MakeStringObject(value),
    }
    result := db.PutIfAbsent(key, entity)
    db.addAof(utils.ToCmdLine3("setnx", args...))
    return protocol.MakeIntReply(int64(result))
}
```

**逐段解说：**

### 参数解析

`args` 是去掉命令名后的字节切片列表。对于 `SETNX key value`，`args[0]` 是 key，`args[1]` 是 value。arity 校验（保证参数数量正确）在框架层已完成，handler 里不需要重复检查。

### 构造 DataEntity 与 Robj

值不是裸的 `[]byte`，而是包在 `database.DataEntity` 里，`Data` 字段是一个 `*object.Robj`。

`object.MakeStringObject`（`internal/object/object.go:100`）会自动选择最紧凑的编码：

- 能解析为整数 → `EncInt`（共享小整数单例，节省分配）
- 长度 ≤ 44 字节 → `EncEmbstr`
- 超过 44 字节 → `EncRaw`

这让 `OBJECT ENCODING` 的输出与真实 Redis 8 保持一致。

### 写入引擎

`db.PutIfAbsent`（`database.go:339`）把 entity 写入底层 dict，仅在 key 不存在时成功，返回 `1`（写入）或 `0`（已存在）。

### AOF 传播

```go
db.addAof(utils.ToCmdLine3("setnx", args...))
```

`addAof`（`database.go:81`）是所有 AOF 写入的**唯一入口**：

```go
func (db *DB) addAof(line CmdLine) {
    if buf, ok := aofBuffers.Load(goid()); ok {
        *buf.(*[]CmdLine) = append(*buf.(*[]CmdLine), line)
        return
    }
    if db.persister != nil {
        db.persister(line)
    }
}
```

逻辑分两路：
- 若当前 goroutine 正在执行 `MULTI` 事务（`aofBuffers` 里有该 goroutine 的缓冲区），则先暂存，等事务提交时批量写入；
- 否则直接调用 `db.persister`（一个函数指针，指向 AOF 追加器）写入磁盘。

这意味着**写命令必须调用 `addAof`**；不调 addAof 的写命令重启后会丢数据。只读命令不需要调用。

### 构造 reply

`protocol.MakeIntReply` 返回 RESP integer 类型回复（`:1\r\n` 或 `:0\r\n`）。常用 reply 构造函数：

| 函数 | RESP 类型 | 适用场景 |
|---|---|---|
| `MakeIntReply(n)` | `:n\r\n` | 整数结果（计数、状态） |
| `MakeBulkReply(b)` | `$len\r\n...\r\n` | 单个字符串值 |
| `MakeMultiBulkReply(bs)` | `*n\r\n...` | 列表结果 |
| `MakeStatusReply("OK")` | `+OK\r\n` | 简单状态 |
| `MakeErrReply("ERR ...")` | `-ERR ...\r\n` | 错误 |
| `&NullBulkReply{}` | `$-1\r\n` | nil bulk（key 不存在） |

---

## 3. 注册命令

在同一文件（`string.go`）的 `init()` 函数里完成注册（`string.go:1087-1093`）：

```go
func init() {
    // ...
    registerCommand("SetNx", execSetNX, writeFirstKey, rollbackFirstKey, 3, flagWrite).
        attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
        attachNotify(notifyString, "set")
    // ...
}
```

说明：

- **`writeFirstKey`**（`tx_utils.go:15`）：`prepare` 函数，把 `args[0]`（即 key）声明为写 key，返回 `([]string{key}, nil)`。事务层据此加写锁。
- **`rollbackFirstKey`**（`tx_utils.go:40`）：`rollback` 函数，先读取 key 的当前状态，生成 `DEL` + 重放命令 + TTL 命令的三条撤销日志，确保 MULTI/EXEC 回滚时能精确还原。
- **`arity = 3`**：命令名 + key + value，恰好 3 个词，正数精确匹配。
- **`attachCommandExtra`**：补充 `COMMAND INFO` 所需的元数据（signs、firstKey/lastKey/keyStep），非必须但建议填写。
- **`attachNotify`**：设置 keyspace 通知的类和事件名，让 `PSUBSCRIBE __keyevent__*:set` 能收到通知。

**新命令注册的最小调用**（不带 extra）：

```go
registerCommand("MyCmd", execMyCmd, writeFirstKey, rollbackFirstKey, 3, flagWrite)
```

只读命令改用 `readFirstKey` 作为 prepare，`nil` 作为 rollback，flags 传 `flagReadOnly`：

```go
registerCommand("MyGet", execMyGet, readFirstKey, nil, 2, flagReadOnly)
```

---

## 4. 差分语料是强制的

差分测试是 hayakv 的核心验收门：`test/diff/` 把命令语料同时发给 hayakv 和真实 Redis 8，逐字节比对回复。

### 覆盖检查会 CI 失败

`test/diff/coverage_test.go:65` 有一个测试：

```go
func TestCorpusMentionsOrExcludesEveryRegisteredCommand(t *testing.T) {
```

它遍历 `database.RegisteredCommandNames()`（`commandinfo.go:131`），如果某个已注册的命令既没有出现在任何语料里、也没有在 `diffExclusions` 映射里给出排除理由，则 `t.Fatalf` 直接让 CI 挂掉。

实际运行效果：

```
$ go test ./test/diff -run TestCorpusMentionsOrExcludesEveryRegisteredCommand -count=1 -v
=== RUN   TestCorpusMentionsOrExcludesEveryRegisteredCommand
--- PASS: TestCorpusMentionsOrExcludesEveryRegisteredCommand (0.00s)
PASS
ok      github.com/amemiya02/hayakv/test/diff   0.426s
```

**添加新命令后，必须二选一**：

**选项 A：在语料文件里加场景**

语料按功能域拆分为 17 个文件（`test/diff/corpus_*_test.go`）：

| 文件 | 适合放入的命令类型 |
|---|---|
| `corpus_base_test.go` | string / list / set / hash / zset 基础操作 |
| `corpus_expiry_test.go` | 过期相关（EXPIRE、TTL、PERSIST …） |
| `corpus_encoding_test.go` | OBJECT ENCODING 场景 |
| `corpus_txn_test.go` | MULTI/EXEC 事务 |
| `corpus_scan_test.go` | SCAN / HSCAN / SSCAN / ZSCAN |
| `corpus_variants_test.go` | 命令变体与边界条件 |
| `corpus_keyspace_test.go` | keyspace 通知 |
| 其他 | pubsub / eval / geo / hashttl / resp3 / 8x / auth / census / cluster / redisdb |

`setnx` 已在 `corpus_base_test.go:40-43`：

```go
{Name: "setnx", Commands: []Command{
    {Args: []string{"SETNX", "k", "v"}},
    {Args: []string{"SETNX", "k", "v2"}},
}},
```

注意：`TestCorpusMentionsOrExcludesEveryRegisteredCommand` 只扫描 `coverage_test.go:67` 列出的这 10 个语料函数（`baseCorpus`、`txnCorpus`、`scanCorpus`、`geoCorpus`、`variantCorpus`、`evalCorpus`、`hashTTLCorpus`、`keyspaceCorpus`、`censusCorpus`、`semantics8xCorpus`）——新增语料函数后，也要把函数指针加进这个列表，否则检查会视为未覆盖。

**选项 B：加进排除清单**

如果命令存在非确定性（随机、时间戳）、阻塞语义、或暂未实现的对比，在 `coverage_test.go:10` 的 `diffExclusions` 映射里加一条说明：

```go
var diffExclusions = map[string]string{
    // ...
    "myrandcmd": "nondeterministic output; requires normalization hook before diffing",
}
```

非确定性命令须先实现归一化钩子（参考差分 harness 中 `INFO` 的处理），再移入语料；不能无限期挂在排除清单里。

---

## 5. 四层测试怎么跑

hayakv 的测试分四层，每层定位不同：

### 第一层：单元测试 + race 检测

```bash
go test -race ./internal/command -run TestSetNX -count=1 -v
```

测试 handler 的逻辑正确性，开 `-race` 检测并发冲突。新命令在 `internal/command/<type>_test.go` 里写单测，参考 `string_test.go:123` 的 `TestSetNX`。

### 第二层：全包 race 测试

```bash
go test -race ./...
```

覆盖所有包，验证没有数据竞争。CI 必跑。

### 第三层：集成测试

```bash
go test ./test/integration -count=1
```

启动真实的 hayakv 进程，用 `redis-cli` 和 `go-redis` 验证连通性与基本协议正确性。

### 第四层：差分测试

```bash
go test ./test/diff -count=1
```

需要 Docker（自动拉起 Redis 8）或已在运行的 Redis 实例：

```bash
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff -count=1
```

字节级对比所有语料命令。这是最终验收门，CI 必跑。

### 第五层（可选）：TCL 测试

```bash
# test/tcl/ 提供了 TCL 测试运行器脚手架，参考 test/tcl/ 目录
```

TCL 套件目前是可选的，主要用于兼容 Redis 官方测试框架。

---

## 6. 提交前 Checklist

在提交 PR 前，逐项确认：

- [ ] **handler** 已实现（`internal/command/<type>.go`），函数签名为 `func execXxx(db *DB, args [][]byte) redis.Reply`
- [ ] **prepare + rollback** 已选择或自定义（写命令至少要有 `writeFirstKey` + `rollbackFirstKey`；只读命令用 `readFirstKey` + `nil`）
- [ ] **注册** 已在同文件 `init()` 中调用 `registerCommand`
- [ ] **arity 和 flags** 正确：arity 含命令名；写命令 `flagWrite`（=0），只读命令 `flagReadOnly`
- [ ] **差分语料或排除清单**：新命令已加入某个 `corpus_*_test.go`，或在 `diffExclusions` 里给出理由；如新建语料函数，已把函数指针加入 `coverage_test.go:67` 的列表
- [ ] **单元测试**：在对应 `_test.go` 文件里有覆盖 happy path 和边界的测试
- [ ] **写命令调用了 `db.addAof`**（只读命令不需要）
- [ ] `gofmt -l ./...` 无输出，`go vet ./...` 无告警
- [ ] 差分测试通过：`go test ./test/diff -run TestCorpusMentionsOrExcludesEveryRegisteredCommand -count=1` → `PASS`

---

*相关阅读：[架构详解](architecture.md)（seam 架构、backends.go 工厂、锁模型）*
