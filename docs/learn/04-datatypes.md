# 第 04 章：list / hash / set / zset 的实现与编码切换

> **前置章节**：[02 — Robj 与对象编码](02-object.md)、[03 — dict 与增量 rehash](03-dict.md)

---

## ① 导读

前两章打下了地基：Robj 是所有值的统一包装（第 02 章），dict 是 hashtable 编码
的底层存储（第 03 章）。本章向上一层，讲四种容器类型——list、hash、set、
zset——如何在「紧凑小结构」与「正式大结构」之间按需切换，以及 hayakv 是如何
逐字节复现这一行为的。

---

## ② Redis 原理

### 2.1 设计哲学：两档结构

Redis 对容器类型的内存优化遵循同一个模式：

- **小数据用紧凑结构**（listpack 或 intset）：元素连续排列在一块内存中，没有
  指针开销，CPU 预取友好，内存占用极低。
- **数据增长后换正式结构**（hashtable / skiplist / quicklist）：保证操作的
  渐近复杂度（O(1) 查找、O(log N) 范围查询等）。
- **切换是单向的**：一旦升级到正式结构，不会因为删除元素而缩回紧凑编码。

阈值由配置控制，默认值在 [`../dev/config.md`](../dev/config.md) 的
ENCODINGS 一节有完整列表。

### 2.2 编码切换总矩阵

| 类型 | 初始编码 | 切换路径 | 元素数阈值（默认） | 配置键 |
|------|----------|----------|--------------------|--------|
| list | listpack | listpack → quicklist | > 128 | `list-max-listpack-size` |
| hash | listpack | listpack → hashtable | > 128 字段 | `hash-max-listpack-entries` |
| set  | intset   | intset → listpack → hashtable | intset > 512；或含非整数 → listpack；listpack > 128 → hashtable | `set-max-intset-entries` / `set-max-listpack-entries` |
| zset | listpack | listpack → skiplist | > 128 成员 | `zset-max-listpack-entries` |

**注意**：set 是三态类型（intset → listpack → hashtable），其余三种类型都是两态。
值长度也可触发切换（`hash-max-listpack-value`、`set-max-listpack-value`、
`zset-max-listpack-value`，默认 64 字节），hayakv 目前仅实现了元素数阈值，
长度阈值尚未接入配置读取（见 `internal/object/encoding.go:283` 注释
`// TODO: Check config thresholds`）。

### 2.3 quicklist：listpack 节点串成的链表

quicklist 是 list 的正式编码。在 Redis 源码中，quicklist 的每个节点是一个
listpack，节点之间用双向链表相连——既保留了 listpack 的内存紧凑性，又避免了单
块 listpack 无限膨胀时的 O(N) 随机访问。

hayakv 的 `internal/datastruct/list/quicklist.go` 实现的是一个「页式」变体：
每个链表节点是一个固定容量（`pageSize = 1024`）的 `[]interface{}` 切片，节点
之间用 `container/list` 双向链表串联。逻辑等价，但不存储 listpack；迭代和
随机访问通过 `iterator`（`quicklist.go:16`）实现，先定位到正确的页，再定位
页内偏移。

### 2.4 zset 的双结构设计

skiplist 编码的 zset 同时维护两个结构：

```
dict:     member → score      O(1) 成员查分（ZSCORE）
skiplist: 按 score 排序的链表  O(log N) 范围查询（ZRANGE、ZRANGEBYSCORE）
```

二者指向同一份 `Element`（member + score），因此写操作需同时更新两处
（`sortedset/sortedset.go:24-38`）：

```go
// internal/datastruct/sortedset/sortedset.go:24
func (sortedSet *SortedSet) Add(member string, score float64) bool {
    element, ok := sortedSet.dict[member]
    sortedSet.dict[member] = &Element{Member: member, Score: score}
    if ok {
        if score != element.Score {
            sortedSet.skiplist.remove(member, element.Score)
            sortedSet.skiplist.insert(member, score)
        }
        return false
    }
    sortedSet.skiplist.insert(member, score)
    return true
}
```

dict 提供 O(1) 点查；skiplist 是一个期望 O(log N) 插入/删除/范围的概率数据
结构，每个节点有多层前向指针，层高由 `randomLevel()` 随机决定。

### 2.5 skiplist 层高直觉

层高的随机函数（`sortedset/skiplist.go:58`）：

```go
func randomLevel() int16 {
    total := uint64(1)<<uint64(maxLevel) - 1  // maxLevel = 16
    k := rand.Uint64() % total
    return maxLevel - int16(bits.Len64(k+1)) + 1
}
```

`bits.Len64(k+1)` 返回 k+1 最高位的位置（1-based）。当 k 均匀分布在
`[0, 2^16 - 2]` 时，结果层高的期望分布近似于每层被选中的概率为 1/2，因此层
高为 h 的概率约为 (1/2)^(h-1)。实际效果：大多数节点只有 1-2 层，极少数节点
达到最高的 16 层。这使得在 N 个节点的 skiplist 中，ZRANGE、ZRANGEBYSCORE
等范围查询的期望时间复杂度为 O(log N)。

---

## ③ hayakv 带读

### 3.1 包结构确认

```
internal/
  datastruct/
    list/        quicklist.go  linked.go（底层 LinkedList，已不用于 list 编码）
    set/         set.go（仅 hashtable 后端，用于旧代码路径）
    sortedset/   sortedset.go  skiplist.go  border.go
    dict/        （第 03 章已讲）
  object/
    encoding.go  ← Hash / Set / ZSet / List 四大容器及其编码切换逻辑全在这里
    object.go    ← Robj、ObjType、Encoding 常量
    intset.go    listpack.go  …
```

所有四种容器类型的编码切换逻辑都集中在 `internal/object/encoding.go`，这是本
章的核心文件。

### 3.2 Hash：listpack → hashtable

`object.Hash` 内部持有两个互斥字段（`encoding.go:34`）：

```go
type Hash struct {
    listpack  *Listpack
    hashtable dict.Dict
    isListpack bool
    fieldExpire map[string]int64
}
```

每次写入时调用 `maybeConvertToHashtable()`（`encoding.go:280`）：

```go
func (h *Hash) maybeConvertToHashtable() {
    // TODO: Check config thresholds
    // For now, convert if we have more than 128 fields
    if h.listpack.Len()/2 > 128 {
        h.convertToHashtable()
    }
}
```

`listpack.Len()` 返回 listpack 中的元素总数（field 和 value 交替存储，所以
除以 2 得字段数）。一旦超过 128，`convertToHashtable()`（`encoding.go:289`）
遍历 listpack，将所有 field-value 对写入新的 `dict.MakeSimple()`，然后置
`isListpack = false`，`listpack = nil`。**转换后 listpack 指针被清空，结构
不可逆。**

命令层（`internal/command/hash.go:89-96`）在 `getOrInitHash` 中创建 Hash 时
初始编码写入 Robj：

```go
robj := &object.Robj{
    Type:     object.TypeHash,
    Encoding: object.EncListpack,
    Ptr:      hash,
}
```

由于 Hash 内部自行管理编码，Robj.Encoding 在写操作后通过
`hash.CurrentEncoding()` 同步（`encoding.go:233`）。

### 3.3 Set：三态切换（intset → listpack → hashtable）

`object.Set` 同时持有三个可选字段（`encoding.go:325`）：

```go
type Set struct {
    intset    *Intset
    listpack  *Listpack
    hashtable dict.Dict
    encoding  string // "intset", "listpack", "hashtable"
}
```

`Add` 方法（`encoding.go:341`）中切换逻辑如下（精简版）：

```go
if s.encoding == "intset" {
    if v, ok := isInt(member); ok {
        if s.intset.Add(v) {
            if s.intset.Len() > 512 {          // 第一阶跃
                s.convertToListpack()
                if s.listpack.Len() > 128 {    // 边界情形
                    s.convertToHashtable()
                }
            }
            return 1
        }
        return 0
    }
    // 非整数 → 立即升级到 listpack
    s.convertToListpack()
    return s.Add(member)
}
if s.encoding == "listpack" {
    s.listpack.AppendStr(member)
    if s.listpack.Len() > 128 {                // 第二阶跃
        s.convertToHashtable()
    }
    return 1
}
```

关键要点：

1. **含非整数 → 立刻跳出 intset**：`isInt` 解析失败就调用
   `convertToListpack()` 再递归 `Add`（`encoding.go:362`）。
2. **513 个整数 → intset 超限**：先升到 listpack，随即检查是否也超过 listpack
   上限（513 > 128，所以直接穿透到 hashtable），`encoding.go:350-354`。
3. **128 个非整数成员 → listpack 超限**：`encoding.go:383-384` 升到
   hashtable。

每次写命令完成后，`syncSetEncodingAfterWrite`（`command/set.go:670`）把
`set.CurrentEncoding()` 同步回 Robj.Encoding，确保 `OBJECT ENCODING` 返回
正确值。

### 3.4 ZSet：listpack → skiplist

`object.ZSet` 内部（`encoding.go:630`）：

```go
type ZSet struct {
    listpack   *Listpack
    skiplist   *SortedSet   // internal/datastruct/sortedset.SortedSet
    isListpack bool
}
```

`Add` 方法在 listpack 路径下写入 member-score 对后检查（`encoding.go:670`）：

```go
if z.listpack.Len()/2 > 128 {
    z.convertToSkiplist()
}
```

`convertToSkiplist()`（`encoding.go:833`）创建新的 `sortedset.Make()`
（内含 dict + skiplist 双结构），遍历 listpack，逐条调用
`sortedset.SortedSet.Add`，最后置 `isListpack = false`，`listpack = nil`。

注意：hayakv 的 listpack 编码 zset 在读操作时**不触发编码切换**——
`getAsZSet`（`command/sortedset.go:33`）和 `getOrInitZSet`（`command/
sortedset.go:58`）仅同步 Encoding 字段，不做任何升级。这是
`TestObjectEncodingHayakv/zset_listpack_survives_reads` 测试场景的保护点
（`test/diff/corpus_encoding_test.go:470`）。

### 3.5 List：listpack → quicklist

`object.List` 内部（`encoding.go:1237`）：

```go
type List struct {
    listpack  *Listpack
    quicklist *QuickList   // internal/datastruct/list.QuickList
    isListpack bool
}
```

`Add` 中（`encoding.go:1264`）：

```go
if l.listpack.Len() > 128 {
    l.convertToQuicklist()
}
```

`convertToQuicklist()`（`encoding.go:1337`）遍历 listpack，将每个元素作为
`[]byte` 追加到新建的 `QuickList`，然后置 `isListpack = false`，
`listpack = nil`。

hayakv 的 `QuickList`（`datastruct/list/quicklist.go`）是页式实现：
每页为容量 1024 的 `[]interface{}` 切片，页间用 Go 标准库
`container/list` 双向链表相连。随机访问通过
`find(index)`（`quicklist.go:53`）实现——先判断 index 在前半还是后半，
再从对应端遍历，找到对应页后取页内偏移。

### 3.6 阈值从哪来

配置键在 `example.conf` 第 92-105 行定义，默认值如下（均为注释掉的示例）：

```
hash-max-listpack-entries 128
set-max-intset-entries    512
set-max-listpack-entries  128
zset-max-listpack-entries 128
list-max-listpack-size    128
```

完整说明见 [`../dev/config.md`](../dev/config.md) 的 ENCODINGS 一节。

阈值的运行机制：`internal/object/thresholds.go` 维护一组原子可替换的
`EncodingThresholds`（默认值与真实 Redis 8 相同），启动时由
`server.NewStorageEngine` 从配置注入；`CONFIG SET hash-max-listpack-entries`
等运行时修改也会即时生效（`internal/command/config_cmd.go` 的
`ApplyEncodingThresholds`）。各转换点统一经 `object.Thresholds()` 读取当前值。
除条目数外，hash/set/zset 的 `*-max-listpack-value` 单值长度维度（默认 64
字节）同样已生效——写入长值会直接翻转编码；list 没有这个维度，与真实
Redis 一致。

> **历史注记**：本章初版写作时，这些阈值还是 `encoding.go` 里的硬编码字面量
> （带 `// TODO: Check config thresholds`），且单值长度维度完全缺失。这两处
> 缺口都是写作本章做实验时暴露的，随后接线修复，并以差分场景
> `encoding_value_thresholds`、`config_set_encoding_threshold`
> （`test/diff/corpus_variants_test.go`）对真实 Redis 8.4 验证。

### 3.7 差分语料佐证

`test/diff/corpus_encoding_test.go` 中有两个直接相关的场景：

**`TestObjectEncodingHayakv`（第 317 行）** 对所有类型和编码逐一断言，包括：

```go
{name: "hash listpack",  ...expected: "listpack"},
{name: "hash hashtable", ...expected: "hashtable"},
{name: "set intset",     ...expected: "intset"},
{name: "set listpack",   ...expected: "listpack"},
{name: "set hashtable",  ...expected: "hashtable"},
{name: "zset listpack",  ...expected: "listpack"},
{name: "zset skiplist",  ...expected: "skiplist"},
{name: "list listpack",  ...expected: "listpack"},
{name: "list quicklist", ...expected: "quicklist"},
```

**`zset_listpack_survives_reads`（第 470 行）** 验证 ZRANGE、ZRANK 等读命令
不改变小 zset 的 listpack 编码——这在 Redis 8 中是正确行为，在旧版 hayakv
（pre-fix）中曾因 `getOrInitSortedSet` 强制升级 skiplist 而失败。

---

## ④ 动手验证

> 先启动服务：`go run ./cmd/hayakv`（监听 `:6399`）

### 4.1 小数据保持紧凑编码

```bash
# list：3 个元素 → listpack
$ redis-cli -p 6399 rpush mylist a b c
(integer) 3
$ redis-cli -p 6399 object encoding mylist
"listpack"

# set：纯整数 → intset
$ redis-cli -p 6399 sadd nums 1 2 3
(integer) 3
$ redis-cli -p 6399 object encoding nums
"intset"

# set：混入非整数 → listpack
$ redis-cli -p 6399 sadd nums hello
(integer) 1
$ redis-cli -p 6399 object encoding nums
"listpack"

# zset：1 个成员 → listpack
$ redis-cli -p 6399 zadd z 1 a
(integer) 1
$ redis-cli -p 6399 object encoding z
"listpack"

# hash：2 个字段 → listpack
$ redis-cli -p 6399 hset myhash f1 v1 f2 v2
(integer) 2
$ redis-cli -p 6399 object encoding myhash
"listpack"
```

### 4.2 list listpack → quicklist（阈值 128）

```bash
$ redis-cli -p 6399 del biglist
$ for i in $(seq 1 129); do redis-cli -p 6399 rpush biglist "item$i" > /dev/null; done
$ redis-cli -p 6399 llen biglist
(integer) 129
$ redis-cli -p 6399 object encoding biglist
"quicklist"
```

129 个元素（> 128）触发转换，编码从 `listpack` 变为 `quicklist`。

### 4.3 zset listpack → skiplist（阈值 128）

```bash
$ redis-cli -p 6399 del bigz
$ for i in $(seq 1 129); do redis-cli -p 6399 zadd bigz $i "m$i" > /dev/null; done
$ redis-cli -p 6399 zcard bigz
(integer) 129
$ redis-cli -p 6399 object encoding bigz
"skiplist"
```

### 4.4 hash listpack → hashtable（阈值 128）

```bash
$ redis-cli -p 6399 del bighash
$ for i in $(seq 1 129); do redis-cli -p 6399 hset bighash "field$i" "value$i" > /dev/null; done
$ redis-cli -p 6399 hlen bighash
(integer) 129
$ redis-cli -p 6399 object encoding bighash
"hashtable"
```

### 4.5 set 三态切换

```bash
# 512 个整数：intset
$ redis-cli -p 6399 del s512
$ for i in $(seq 1 512); do redis-cli -p 6399 sadd s512 $i > /dev/null; done
$ redis-cli -p 6399 object encoding s512
"intset"

# 加第 513 个整数：intset 超限 → listpack 超限 → 直接落在 hashtable
$ redis-cli -p 6399 sadd s512 513
(integer) 1
$ redis-cli -p 6399 object encoding s512
"hashtable"
```

注：513 > 512 触发 intset → listpack；513 > 128 立刻触发 listpack → hashtable，
因此外部观察不到中间的 listpack 状态。

### 4.6 差分测试

在仓库根目录运行（无 Redis 实例时差分部分会 skip，hayakv-only 场景仍执行）：

```bash
$ go test ./test/diff -run TestObjectEncoding -count=1 -v
=== RUN   TestObjectEncodingHayakv/hash_listpack
--- PASS
=== RUN   TestObjectEncodingHayakv/hash_hashtable
--- PASS
=== RUN   TestObjectEncodingHayakv/set_intset
--- PASS
=== RUN   TestObjectEncodingHayakv/set_listpack
--- PASS
=== RUN   TestObjectEncodingHayakv/set_hashtable
--- PASS
=== RUN   TestObjectEncodingHayakv/zset_listpack
--- PASS
=== RUN   TestObjectEncodingHayakv/zset_skiplist
--- PASS
=== RUN   TestObjectEncodingHayakv/list_listpack
--- PASS
=== RUN   TestObjectEncodingHayakv/list_quicklist
--- PASS
=== RUN   TestObjectEncodingHayakv/zset_listpack_survives_reads
--- PASS
=== RUN   TestObjectEncodingHayakv/hash_listpack_survives_reads
--- PASS
=== RUN   TestObjectEncodingHayakv/listpack_survives_aof_rewrite
--- PASS
--- PASS: TestObjectEncodingHayakv (1.15s)
ok  	github.com/amemiya02/hayakv/test/diff	1.540s
```

如果环境中没有 Redis 8 实例，`TestObjectEncodingDiff` 会打印 `skip`——skip
输出本身说明了两件事：测试框架已就绪，只是差分部分依赖真实 Redis 才能运行。

---

## ⑤ 延伸阅读

- [redis.io — Data types](https://redis.io/docs/manual/data-types/)：各类型官方
  文档，含编码决策说明
- Redis 源码 `t_hash.c`、`t_zset.c`：可直接对照 hayakv 阅读，理解双结构
  维护细节
- [`../dev/config.md`](../dev/config.md) ENCODINGS 一节：hayakv 可配置的阈值
  键名与默认值
- **下一章**：[05 — 网络模型：goroutine vs 单线程事件循环](05-event-loop.md)
  （规划中）
