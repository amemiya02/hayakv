# 第 02 章：Robj 与对象编码

> **系列**：用 Go 重写 Redis — hayakv 代码导读

---

## 1. 本章导读

上一章（[第 01 章](01-resp.md)）讲了 Redis 的有线协议：命令以 RESP 格式从客户端
传到服务端，服务端解析后执行。本章向里走一层，看服务端拿到命令参数后，是如何
**把值存起来**的。

Redis 的核心抽象是**对象（Robj）**：每个 value 不只是一块字节，而是携带了
类型（string/list/hash/set/zset）和**编码**（实际存储格式）的包装结构。理解
这一层是读懂 Redis 任何命令实现的前提。

**你将学到：**

- Redis 对象模型的设计动机——为什么同一种类型要有多种编码；
- string 的三种编码（int / embstr / raw）以及 44 字节阈值的来历；
- listpack：取代 ziplist 的紧凑序列格式，为何 O(n) 也快；
- intset：有序整数集合如何按需升级位宽；
- 共享对象池：小整数单例的工程意义；
- hayakv 如何让 `OBJECT ENCODING` 与真实 Redis 8.x 逐字节一致；
- 一个值得关注的行为差异：`APPEND` 后 hayakv 的编码与真实 Redis 不同。

**前置知识：** [第 01 章：RESP2/RESP3 协议](01-resp.md)。

---

## 2. Redis 原理

### 2.1 对象模型：type + encoding + ptr

Redis 内部用一个名为 `robj`（Redis Object）的结构包装所有 value：

```
robj {
    type     : string | list | hash | set | zset
    encoding : int | embstr | raw | listpack | quicklist | intset | hashtable | skiplist | ...
    ptr      : 指向实际数据
}
```

**type** 是用户可见的语义类型，决定了哪些命令可以操作这个 key（`HSET` 只能用于
hash，`ZADD` 只能用于 zset）。

**encoding** 是存储格式，对用户透明，但对内存和性能影响巨大。同一个 type 可以有
多种 encoding，Redis 在运行时根据元素数量和元素大小动态切换：

- **小数据用紧凑格式**（listpack、intset、embstr）：连续内存、节省指针开销、
  对 CPU cache 友好，代价是部分操作 O(n)。
- **数据变大后换回正式结构**（hashtable、skiplist、quicklist、raw）：O(log n)
  或 O(1) 的操作复杂度，代价是额外的指针和元数据开销。

这种"小时用紧凑格式、大了换正式结构"的策略，是 Redis 内存效率高的核心原因之一。
`OBJECT ENCODING key` 命令可以查询当前 encoding，是调优的重要工具。

### 2.2 string 三态：int / embstr / raw

string 类型根据值的内容和长度选择三种编码：

#### int

值能被解析为 `int64`，且表示为十进制字符串后与原始字节完全一致（round-trip
验证），则使用 `int` 编码，直接把 `int64` 存入 `ptr`，完全不需要字符串分配。
加减法（`INCR`/`DECR`）直接操作整数，无需解析字符串。

#### embstr（嵌入字符串）

值不是整数，且字节长度 **≤ 44**，使用 `embstr` 编码。这个 44 字节阈值来自
真实 Redis 的 cache line 算术：Redis 的 `robj` 头部占 16 字节，SDS（动态字符串）
头部占 4 字节（`sdshdr8`），再加 1 字节 null 终止符，合计 21 字节。嵌入字符串
和 robj 在**同一次 `malloc`** 中分配，整块不超过 64 字节（一个 CPU cache line），
因此最大可嵌入 `64 - 16 - 4 = 44` 字节的有效载荷。

好处：单次分配、单次释放，访问 robj 和字符串内容只需一次 cache line 加载。
坏处：一旦执行修改（`APPEND`、`SETRANGE`），Redis 会立即把它**升级为 raw**，
因为 embstr 被设计为不可变的——升级之后不再降回 embstr。

> **注意：hayakv 的差异**——见第 4 节"动手验证"。

#### raw

值是长度 > 44 字节的普通字符串，或由 embstr 升级而来。`ptr` 指向独立分配的
字节切片，支持原地修改。

### 2.3 listpack：连续内存的紧凑序列

listpack 是 Redis 7.0 用来取代 ziplist 的格式（Redis 源码：`listpack.c`）。
核心思路：**把所有元素打包进一个连续的字节数组**，每个元素自带编码头。

元素布局（hayakv 版本）：

```
[ tag (1 byte) ] [ length/value (varint) ] [ raw bytes ]
```

- 字符串元素：`0x00` + uvarint(长度) + UTF-8 字节序列
- 整数元素：`0x01` + zigzag varint（节省负数空间）

连续内存带来三个好处：

1. **CPU cache 友好**：顺序遍历时几乎没有 cache miss；
2. **无指针开销**：省去链表每个节点的 prev/next 指针（各 8 字节）；
3. **紧凑存储**：数字用 varint，短字符串用 uvarint 长度前缀，都比固定宽度省空间。

代价是随机访问为 O(n)——必须从头顺序解码。但 Redis 对小型 hash / set / zset /
list 的典型操作（遍历所有字段、按值查找）在元素少时比 O(1) 的哈希表更快，
因为哈希表的常数因子（内存分配、指针追踪）在小数据时反而更贵。

当元素数量或单个元素大小超过阈值（hash-max-listpack-entries 等配置项），
Redis 把 listpack 转换为正式结构，**转换单向不可逆**。

### 2.4 intset：有序整数集合与按需位宽升级

当 set 的所有成员都是整数时，Redis 使用 `intset`：一个**有序的整数数组**，
用二分查找做 O(log n) 的成员测试和插入。

intset 的内存宽度按需升级：

- 初始用 **16 位**（`int16_t`），支持 −32768 ~ 32767；
- 遇到超出范围的整数时升级为 **32 位**（`int32_t`）；
- 再次超出时升级为 **64 位**（`int64_t`）；
- **升级不可逆**：即便删去了大整数，宽度也不会缩回。

数组始终有序，这使得 `SRANDMEMBER`、`SMEMBERS` 等命令的输出是确定性的，
对差分测试（见第 10 章）非常友好。

超过 512 个成员（`set-max-intset-entries` 默认值）或插入非整数成员时，
intset 转换为 listpack 或 hashtable。

### 2.5 共享对象：小整数单例池

Redis 在启动时预分配 **0 ~ 9999** 的整数 `robj` 单例，存入全局数组。
每次需要这个范围内的整数时直接返回指针，不做任何分配。

在高并发场景下，大量 key 的 value 是计数器、ID、状态码，这些值集中在
小整数范围内。共享对象池把这些 `robj` 的分配次数降为零，同时也节省了内存
（10000 个对象只需一份存储）。

---

## 3. hayakv 实现带读

### 3.1 Robj 结构定义

`internal/object/object.go` 定义了 hayakv 的对象模型：

```go
// ObjType represents the type of a Redis object
type ObjType int

const (
    TypeString ObjType = iota
    TypeList
    TypeSet
    TypeHash
    TypeZSet
    TypeStream
)

// Encoding represents the encoding of a Redis object
type Encoding int

const (
    EncInt        Encoding = iota // int64
    EncEmbstr                     // embedded string (<=44 bytes)
    EncRaw                        // raw string (>44 bytes)
    EncListpack                   // listpack
    EncListpackEx                 // listpack with field expiries
    EncQuicklist                  // quicklist
    EncIntset                     // intset
    EncHashtable                  // hashtable/dict
    EncSkiplist                   // skiplist (for sorted set)
    EncStream                     // stream
)

// Robj is a Redis object with type, encoding, and pointer to actual data
type Robj struct {
    Type     ObjType
    Encoding Encoding
    Ptr      interface{}
}
```

Go 的 `interface{}` 承担了 C 语言 `union ptr` 的角色：同一字段可以存 `int64`、
`[]byte`、`*Listpack`、`*Intset` 等任意类型。`TypeName()` 和 `EncodingName()`
方法把内部枚举映射为 Redis 协议中使用的字符串（`"string"`、`"embstr"` 等），
供 `OBJECT ENCODING` 命令直接返回。

hayakv 比真实 Redis 多了一个 `EncListpackEx` 编码（带字段级过期的 listpack），
这是 Redis 8.x 的 `HEXPIRE` 功能所引入的内部格式，`OBJECT ENCODING` 对外仍
报告为 `"listpack"`（由 `Hash.CurrentEncoding()` 决定）。

### 3.2 MakeStringObject：三分支判断

`object.go` 第 100–121 行的 `MakeStringObject` 函数实现了 string 编码选择：

```go
func MakeStringObject(b []byte) *Robj {
    // Try to parse as int
    if v, ok := isInt(string(b)); ok {
        return SharedInt(v)
    }

    // Check if it's short enough for embstr (<=44 bytes)
    if len(b) <= 44 {
        return &Robj{
            Type:     TypeString,
            Encoding: EncEmbstr,
            Ptr:      b,
        }
    }

    // Otherwise, raw string
    return &Robj{
        Type:     TypeString,
        Encoding: EncRaw,
        Ptr:      b,
    }
}
```

第一分支先调用 `isInt`（`listpack.go` 第 74–88 行）：除了用 `parseInt64` 解析，
还做 **round-trip 验证**——把解析结果再格式化回字符串，必须与原始字节完全相同。
这样可以正确拒绝 `+1`、`01`、`1e2` 等虽然表示整数但 Redis 不会用 `int` 编码的
值。`isInt` 成功后调用 `SharedInt(v)`，对 0–9999 的值直接返回共享单例。

第二分支用字节长度与 44 比较，对应 2.2 节讲的 cache line 算术。`len(b) <= 44`
完全匹配真实 Redis 的阈值，因此 `OBJECT ENCODING` 的结果与 Redis 8.x 一致。

### 3.3 listpack.go：元素编码、追加与遍历

`internal/object/listpack.go` 实现了紧凑字节数组。核心类型：

```go
type Listpack struct {
    buf []byte  // 连续字节缓冲
    num int     // 元素数量（不需扫描 buf 就能得到长度）
}
```

**追加**（`AppendStr` / `AppendInt`，第 182–191 行）直接调用编码函数把字节
追加到 `buf` 末尾，再递增 `num`。没有额外的元数据头或尾。

**元素编码格式**（`encodeStrElem` / `encodeIntElem`，第 16–31 行）：

- 字符串：`0x00` + `uvarint(len)` + 原始字节。1 字节 tag + 最多 10 字节长度 + 内容。
- 整数：`0x01` + zigzag varint。负数用 zigzag 编码（-1 编码为 1，-2 编码为 3），
  让绝对值较小的负数也能用短字节表示。

**遍历**（`Range`，第 214–226 行）：从 `pos=0` 开始，每次调用 `decodeElem`
解码一个元素并得到消耗的字节数，再前进 `pos`。随机访问 `Get(i)` 需要先遍历
前 i 个元素（第 194–209 行），因此是 O(n)——这也是 listpack 只用于小数据的
根本原因。

注意 hayakv 的 listpack 没有实现真实 Redis 的**回填长度（prevlen）**字段。
真实 Redis listpack 每个元素末尾存储本元素的字节长度，允许从后往前遍历
（用于 `LPUSH` 等操作）。hayakv 为了简洁选择了单向遍历，对已实现的命令
集合已经足够。

### 3.4 intset.go：有序数组与位宽升级

```go
type Intset struct {
    data  []int64  // 有序整数数组（始终升序）
    width int      // 16, 32, or 64
}
```

`Add`（第 44–67 行）用 `sort.Search` 做二分查找定位插入点，去重后插入。
插入前调用 `updateWidth`（第 103–115 行）检查是否需要位宽升级：

```go
func (is *Intset) updateWidth(v int64) {
    if v < -2147483648 || v > 2147483647 {
        is.width = 64
    } else if v < -32768 || v > 32767 {
        if is.width < 32 {
            is.width = 32
        }
    } else {
        if is.width < 16 {
            is.width = 16
        }
    }
}
```

阈值与真实 Redis 一致：16 位覆盖 ±32767，32 位覆盖 ±2147483647，超出用 64 位。
`width` 字段只增不减——删除大整数后宽度也不会缩回，这与真实 Redis 的行为相同。

hayakv 的 `Intset` 在内部用 `[]int64` 存储所有元素（而不是真的按 width 打包字节），
`width` 字段当前仅用于记录已见到的最大范围，主要用于 `OBJECT ENCODING` 的逻辑
（`EncIntset`）。真实 Redis 的 intset 会根据 width 实际以 2/4/8 字节紧凑打包，
hayakv 在这里选择了工程简洁性优先。

### 3.5 shared.go：共享小整数池

`internal/object/shared.go` 忠实复刻了 Redis 的 `OBJ_SHARED_INTEGERS`：

```go
var sharedIntegers [10000]*Robj

func init() {
    for i := 0; i < 10000; i++ {
        sharedIntegers[i] = &Robj{
            Type:     TypeString,
            Encoding: EncInt,
            Ptr:      int64(i),
        }
    }
}

func SharedInt(n int64) *Robj {
    if n >= 0 && n < 10000 {
        return sharedIntegers[n]
    }
    return &Robj{
        Type:     TypeString,
        Encoding: EncInt,
        Ptr:      n,
    }
}
```

共享范围是 **0 ~ 9999**，与真实 Redis（`OBJ_SHARED_INTEGERS = 10000`）完全
一致。`MakeStringObject` 在 `isInt` 成功时直接调用 `SharedInt`，使得凡是
存储小整数值的命令（`SET k 42`、`INCR`、`LPUSH` 等等）都不会触发堆分配。

这一功能是 hayakv 性能优化里程碑的成果之一：在高频写入场景下，消除小整数
对象的 GC 压力是可测量的。

### 3.6 OBJECT ENCODING 的实现路径

`OBJECT ENCODING` 命令（`internal/command/` 中的 `execObjectEncoding`）最终
调用 `Robj.EncodingName()`，直接返回 `Encoding` 枚举映射的字符串。从值写入到
编码查询的完整路径是：

```
SET k v
  → MakeStringObject([]byte("v"))          # object.go
    → isInt / len check / SharedInt        # listpack.go / shared.go
      → Robj{Type, Encoding, Ptr}
        → db.PutEntity(key, &DataEntity{Data: robj})

OBJECT ENCODING k
  → robj.EncodingName()                    # object.go:68
    → "int" / "embstr" / "raw" / ...
```

差分测试语料库 `test/diff/corpus_encoding_test.go` 验证这条路径的正确性。
例如第 364–372 行覆盖了 embstr / raw 的判定：

```go
{
    name:  "string embstr",
    setup: []string{"SET", "test:embstr", "hello"},
    key:   "test:embstr",
    expected: "embstr",
},
{
    name:  "string raw",
    setup: []string{"SET", "test:raw", "this is a long string that exceeds 44 bytes for testing raw encoding"},
    key:   "test:raw",
    expected: "raw",
},
```

测试同时验证 hash / set / zset / list 在小元素数时保持 `listpack` 编码（第
377–427 行），以及 read 命令不会意外触发编码转换（`zset_listpack_survives_reads`
/ `hash_listpack_survives_reads`，第 468–566 行）。

---

## 4. 动手验证

先构建并启动 hayakv（侦听 6399 端口）：

```bash
go build -o hayakv ./cmd/hayakv && ./hayakv &
```

### 4.1 基本编码验证

```
$ redis-cli -p 6399 set n 123 && redis-cli -p 6399 object encoding n
OK
int

$ redis-cli -p 6399 set s hello && redis-cli -p 6399 object encoding s
OK
embstr

$ redis-cli -p 6399 set l "$(printf 'x%.0s' {1..64})" && redis-cli -p 6399 object encoding l
OK
raw
```

### 4.2 44/45 字节边界实验

```
$ redis-cli -p 6399 set b44 "$(printf 'a%.0s' {1..44})" && redis-cli -p 6399 object encoding b44
OK
embstr

$ redis-cli -p 6399 set b45 "$(printf 'a%.0s' {1..45})" && redis-cli -p 6399 object encoding b45
OK
raw
```

正好 44 字节 → `embstr`，45 字节 → `raw`，与 Redis 8.x 完全一致。

### 4.3 APPEND 行为差异

```
$ redis-cli -p 6399 set s hello && redis-cli -p 6399 object encoding s
OK
embstr

$ redis-cli -p 6399 append s world && redis-cli -p 6399 object encoding s
10
embstr
```

**真实 Redis 的行为：** `APPEND` 之后 encoding 会变成 `raw`，因为 Redis 将
embstr 视为不可变，任何修改都强制升级。

**hayakv 的当前行为：** `execAppend`（`internal/command/string.go` 第 686–698
行）把拼接后的字节传回 `MakeStringObject`，后者重新判断编码。由于
`"helloworld"` 是 10 字节（≤ 44 且不是纯整数），所以得到 `embstr`——没有保留
"经过修改就升级为 raw"的语义。

这是 hayakv 与真实 Redis 的一处已知行为差异。在差分测试语料中，`APPEND` 后
查询 `OBJECT ENCODING` 的用例尚未被覆盖，感兴趣的读者可以将其作为贡献点。

### 4.4 运行对象包单元测试

```
$ go test -race ./internal/object/ -count=1
ok  	github.com/amemiya02/hayakv/internal/object	1.448s
```

测试覆盖了 listpack 的编码/解码轮转、intset 的插入与升级逻辑、MakeStringObject
的三分支判定，以及共享整数的引用一致性。

清理：

```bash
kill %1          # 停止 hayakv
rm -f hayakv dump.rdb
```

---

## 5. 延伸阅读

- **Redis 官方文档**：[OBJECT ENCODING 命令参考](https://redis.io/docs/latest/commands/object-encoding/)
  — 完整列出每种 type 的合法 encoding 及触发条件。
- **Redis 源码**：`object.c`（`createStringObject`、`getDecodedObject`）和
  `listpack.c`（真实 listpack 格式含 prevlen 字段的完整实现）。
- **hayakv 差分语料**：`../../test/diff/corpus_encoding_test.go`
  — 查看 `TestObjectEncodingDifferential`（对比真实 Redis）与
  `TestObjectEncodingHayakv`（独立验证）的用例，了解哪些编码场景已通过逐字节
  一致性验证。
- **下一章**：[第 03 章：dict 与增量 rehash](03-dict.md) — 当 hash / set 超出
  listpack 阈值后，数据会落入 `dict`（哈希表）。下一章深入 dict 的实现，包括
  增量 rehash 如何在不阻塞命令的前提下完成扩缩容。
