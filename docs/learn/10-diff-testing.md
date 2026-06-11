# 第 10 章：差分测试——如何验证「逐字节一致」

> **前置章节**：本章是方法论收官章，前置无硬性要求。读完 01–09 章后阅读
> 效果最佳，但也可作为独立的测试方法论参考。

---

## 1 导读

前九章沿着「协议 → 对象模型 → 数据结构 → 网络层 → 持久化 → 复制 → 集群」
的轴线，把 Redis 8.x 的主要内核逐件拆解。每一章的结尾都有一句隐含的承诺：
「hayakv 的行为与真实 Redis 相同」。

但「相同」要怎么证明？

文档可以写错，记忆会漂移，单元测试只验证作者以为正确的行为。本章要回答的
问题是：在重写一个已有系统时，最强的可机检验收定义是什么，以及 hayakv 的
测试体系怎样把这个定义落进代码与 CI。

---

## 2 原理：重写系统的验收方法论

### 2.1 验收难题

重写一个有真实用户、有既定语义的系统，验收是最难的部分。难在哪里？

- **文档不完整**：Redis 文档描述的是理想语义，边角情况往往靠源码注释和 issue 才能读懂。
- **记忆靠不住**：「我记得 INCR 遇到非整数会返回错误」——这句话在重写时几乎
  每次都是正确的，但偶尔某个命令某个选项就不是，而且作者完全意识不到自己记错了。
- **单元测试验证的是意图，不是忠实度**：一个测试写
  `assert GET("k") == "v"` 的时候，它验证的是作者认为正确的回复。
  如果作者的认知本身有偏差，测试会和实现一起通过，并且一起错。

解法是引入一个无争议的参照系：**以真实 Redis 为 oracle，把两端的线路回复
逐字节比对，只要一致，就算正确**。

这个定义有几个优点：

1. **可机检**：计算机做字节比较，不需要人判断「这个错误信息算不算等价」。
2. **不依赖对语义的理解**：oracle 说什么正确，就是正确。
3. **发现的 bug 是真实的 bug**：如果两端回复不同，至少有一端错了。

### 2.2 差分测试与单元测试的互补

单元测试和差分测试抓不同种类的 bug，缺一不可。

**单元测试能抓、差分测试抓不到的 bug**：

- 内部不变量（如 rehash 过程中某个指针为空）——差分只看线路回复，看不到
  进程内部崩溃之前的状态。
- 并发安全——两端各自单线程运行时，race detector 是唯一能发现竞争的工具。
- 性能回归——差分不测延迟，单测可以。

**差分测试能抓、单元测试永远抓不到的 bug**：

- 作者的认知偏差。你以为 `OBJECT ENCODING` 在 31 字节的字符串上应该回复
  `embstr`，你把这个写进了单测，单测通过了，但真实 Redis 的阈值是 44 字节。
  差分会立刻抓到这个偏差，单测永远不会。
- 版本漂移。Redis 8.4 在某个命令上悄悄改了回复格式，单测感知不到，差分
  对着新版本跑一次就暴露了。
- 协议细节。RESP 回复的类型前缀、CRLF、整数 vs 批量字符串——格式级别的
  偏差只有逐字节比对才能抓到。

### 2.3 逐字节比较的天敌：非确定性

把「逐字节一致」当验收标准，立刻遇到一类无法消除的对手：**非确定性回复**。

`INFO server` 里的 `uptime_in_seconds`、`TIME` 命令返回的微秒时间戳、
`RANDOMKEY` 的随机结果、`OBJECT ENCODING` 可能在不同内存压力下给出不同编码——
这些命令的回复在两个独立进程之间天然不一样，逐字节比对必然失败，不是 bug。

有两种处理手段，各有代价：

**归一化钩子（normalization hook）**：在比较前对两端的回复应用同一个变换函数，
抹掉非确定性的部分，保留有意义的语义。例如把 TTL 的具体秒数替换为「正整数」，
把 SCAN 的游标替换为 `0` 并排序返回的 key 列表。归一化保留了命令覆盖，
是「偿还技术债」的方式。

**显式排除清单（exclusion list）**：把命令从差分语料库中除名，附上理由。
用于真正无法归一化的命令（`RANDOMKEY`）、或者尚未处理的技术债
（`INFO` 需要字段级归一化，工作量较大）。排除是「承认欠债」——语料库
不再对这条命令的忠实度作担保，需要其他测试层兜底。

这两种手段的关系：**排除是债，钩子是偿还**。一条命令从排除清单毕业、
进入语料库并带上归一化钩子，意味着这条命令的忠实度回到了差分验收范围内。

### 2.4 覆盖强制：让「忘了加差分」不可能发生

差分语料库有一个隐蔽的漏洞：开发者实现了一条新命令，注册进命令表，
然后忘了在语料库里添加对应的场景。CI 仍然全绿——因为没有测试去跑它。

hayakv 的解法是 `TestCorpusMentionsOrExcludesEveryRegisteredCommand`
（`test/diff/coverage_test.go:65`）。它在运行时：

1. 扫描所有语料库函数，收集出现过的所有命令名。
2. 调用 `database.RegisteredCommandNames()` 获取全部已注册命令。
3. 对每一条注册命令，检查：要么语料库里出现过，要么在 `diffExclusions` 里
   有明确的排除理由。两者都没有 → 测试失败。

这把「忘记」变成了 CI 级别的失败——和编译错误一样无法忽视。开发者添加命令
时必须二选一：写差分场景，或者在排除清单里写明理由。两者都是显式决策，
不存在静默通过的漏洞。

### 2.5 TCL 套件：第三重保险

差分测试是对真实 Redis 的「影子跑」，覆盖的是 hayakv 自己能表达的命令序列。
Redis 官方的 TCL 测试套件（`redis/tests/`）是另一个维度的资产：它是 Redis
开发者在写命令实现时同步写下的场景，覆盖了大量 hayakv 开发者可能想不到的
边角情况。

把这套 TCL 套件跑在 hayakv 上，相当于借用了整个 Redis 社区多年积累的
测试智慧。挑战在于：TCL 套件体量巨大（数百个文件），短期内不可能全部通过。

解法是**渐进收编（scoreboard 模式）**：用 `manifest.yaml` 维护一份治理台账，
每个文件标注 `pass`、`partial`（部分通过，列出跳过项）或 `excluded`（明确
排除，附理由）。每次 nightly 运行把通过率、命令数、排除数快照进
`scoreboard/history.jsonl`，趋势一目了然。台账本身受覆盖检查保护，
in-scope 文件必须在台账里出现，不允许静默缺席。

### 2.6 方法论总结：四个要素

这套验收体系的方法论可以提炼为四个要素，适用于任何「重写既有系统」的场景：

| 要素 | 作用 |
|------|------|
| **Oracle** | 以真实系统为参照系，给「正确」下操作性定义 |
| **逐字节比对** | 避免人工判断引入主观偏差，让不一致无处可藏 |
| **覆盖强制** | 让「忘了测」在 CI 不可能静默通过 |
| **归一化** | 把非确定性收纳进已知边界，而不是无限扩大排除清单 |

没有 oracle，测试验证的只是作者的意图；没有覆盖强制，语料库会悄悄萎缩；
没有归一化，排除清单会无限膨胀直到失去意义。四者缺一，验收就出现了裂缝。

---

## 3 hayakv 带读

### 3.1 harness_test.go：双端回放核心

差分测试的基础设施集中在 `test/diff/harness_test.go`。

**Redis 来源三级**（`startRedis8`，第 233 行）：

```go
func startRedis8(t *testing.T) (string, func()) {
    if addr := os.Getenv("HAYAKV_DIFF_REDIS_ADDR"); addr != "" {
        waitForPing(t, addr)
        return addr, func() {}
    }
    if _, err := exec.LookPath("docker"); err != nil {
        t.Skip("docker not installed; ...")
    }
    // ... docker run redis:8 ...
}
```

优先级：环境变量指定的外部地址 → 自动拉起 Docker 容器 → 两者均无则 `t.Skip`。
这个三级降级保证了本地、CI、受限环境都能正确处理，不会因为没有 Redis 而
报错——跳过是诚实的，失败才需要调查。

**runScenario：单次回放**（第 330 行）：

```go
func runScenario(t *testing.T, addr string, scenario Scenario) [][]byte {
    conn, _ := net.DialTimeout("tcp", addr, 2*time.Second)
    defer conn.Close()
    reader := bufio.NewReader(conn)
    replies := make([][]byte, 0, len(scenario.Commands))
    // 每次 scenario 前先 FLUSHALL，保证隔离
    conn.Write(encodeCommand([]string{"FLUSHALL"}))
    readReply(reader)
    for _, cmd := range scenario.Commands {
        conn.Write(encodeCommand(cmd.Args))
        reply, _ := readReply(reader)
        replies = append(replies, reply)
    }
    return replies
}
```

对 hayakv 和 Redis 各调用一次，返回回复字节切片列表。

**assertReplyEqual：带钩子的比较**（第 405 行）：

```go
func assertReplyEqual(t *testing.T, sc Scenario, h, r [][]byte) {
    for i := range h {
        hi, ri := h[i], r[i]
        if fn := sc.Commands[i].Normalize; fn != nil {
            hi, ri = fn(hi), fn(ri)
        }
        if !bytes.Equal(hi, ri) {
            t.Fatalf("cmd %v\nhayakv: %q\nredis:  %q", ...)
        }
    }
}
```

每条命令可以携带一个 `Normalize func([]byte) []byte`，在比较前对两端的
原始回复字节施加相同变换。没有钩子时退化为纯字节比较。

### 3.2 normalizeTTL：归一化钩子实例

`test/diff/corpus_8x_test.go` 第 11 行定义了 `normalizeTTL`：

```go
func normalizeTTL(raw []byte) []byte {
    if len(raw) == 0 || raw[0] != ':' {
        return raw
    }
    idx := bytes.IndexByte(raw, '\r')
    s := string(raw[1:idx])
    if s == "0" || s == "-1" || s == "-2" {
        return raw
    }
    return []byte(":1\r\n")
}
```

**为什么需要它？** `MSETEX` 带 `EX 100` 时，hayakv 和 Redis 在同一个测试
循环里几乎同时设置了到期时间。随着测试时序的微小抖动，`TTL k` 可能回复
`100` 或 `99`。两个数字都是「正确」的，但逐字节比对会让测试概率性失败。
归一化钩子把「任何正整数 TTL」统一规约为 `1`，保留了语义（key 还有 TTL）
但消除了时序噪声。

`normalizeScan`（`corpus_base_test.go:72`）同理：SCAN 的游标和返回 key
的顺序在两端都是实现细节，归一化函数把游标替换为 `0`，对 key 列表排序，
让比对只验证「哪些 key 存在」而不是「以什么顺序返回」。

### 3.3 coverage_test.go：覆盖强制解剖

`test/diff/coverage_test.go` 的核心结构：

```go
var diffExclusions = map[string]string{
    "blpop":  "blocking command; timeout semantics are not byte-diffable in this harness",
    "info":   "contains server-specific fields that need normalization",
    "keys":   "unordered output is unsuitable for byte-for-byte corpus without normalization",
    "randomkey": "nondeterministic by design",
    // ... 共 40+ 条排除
}

func TestCorpusMentionsOrExcludesEveryRegisteredCommand(t *testing.T) {
    covered := map[string]bool{}
    corpora := []func() []Scenario{baseCorpus, txnCorpus, scanCorpus, ...}
    for _, corpus := range corpora {
        for _, scenario := range corpus() {
            for _, cmd := range scenario.Commands {
                covered[strings.ToLower(cmd.Args[0])] = true
            }
        }
    }
    for _, name := range database.RegisteredCommandNames() {
        if !covered[name] {
            if reason := diffExclusions[name]; reason == "" {
                t.Fatalf("registered command %q has no diff scenario or exclusion reason", name)
            }
        }
    }
}
```

排除清单里的每一条都有明确理由。两条典型示例：

- `"blpop"` → `"blocking command; timeout semantics are not byte-diffable in this harness"`：
  阻塞命令依赖超时语义，两端的阻塞时间无法精确同步，排除是正确选择。
- `"keys"` → `"unordered output is unsuitable for byte-for-byte corpus without normalization"`：
  `KEYS` 返回无序结果，需要归一化但尚未实现，诚实标记为排除债。

### 3.4 教学案例：commit 7fd1ff2 ——差分抓到的真实 bug

这是整套方法论最生动的示例。

**背景**：hayakv 实现了 Redis 8.4 新增的 `IFDEQ`/`IFDNE` 条件写命令——
写入或删除时先对当前值求哈希摘要，再与客户端提供的摘要比较。实现者选择了
XXH3-128（128 位哈希，输出 32 个十六进制字符）。单元测试：写了若干场景，
断言 `SET k v IFDEQ <digest>` 返回预期结果。全部通过。

**差分揭露**：把相同场景跑在真实 Redis 8.4 上，所有摘要相关场景全部失败。
错误形态：

```
hayakv: "+OK\r\n"
redis:  "-ERR invalid digest length, expected 16 hex chars\r\n"
```

**根因**（`git show 7fd1ff2`）：

```diff
-// Uses XXH3-128 (zeebo/xxh3) to match Redis 8.4's digest algorithm.
+// Uses XXH3-64 (zeebo/xxh3) producing 16 hex chars, matching Redis 8.4.

-func ValueDigest(b []byte) string {
-    h := xxh3.Hash128(b)
-    var buf [16]byte
-    binary.BigEndian.PutUint64(buf[0:8], h.Hi)
-    binary.BigEndian.PutUint64(buf[8:16], h.Lo)
-    return hex.EncodeToString(buf[:])  // 32 hex chars
+func ValueDigest(b []byte) string {
+    h := xxh3.Hash(b)
+    return fmt.Sprintf("%016x", h)    // 16 hex chars
 }
```

实现者读规范时误记了哈希位宽：128 位（32 hex）vs 真实 Redis 要求的 64 位（16 hex）。
单元测试的断言是「给定值 `hello` 的摘要是 `<32字符串>`」——这个断言是根据
实现算出来的，它验证的是「实现前后一致」，不是「与 Redis 一致」。

**单元测试永远抓不到这个 bug**，因为单测编码的是作者自己的误解。只有对着
真实 Redis 的 oracle，差分比对才能把「你的摘要格式根本不对」这件事讲清楚。

修复后，commit message 写道：「Verified: all 28 8.x diff corpus scenarios pass
against real redis:8 (Docker).」——差分通过，即验收通过。

### 3.5 test/tcl/ 结构速览

TCL 套件的基础设施由四个文件组成：

**`redisversion.go`**（第 7–10 行）：

```go
const (
    RedisImageTag = "redis:8.4.2"
    RedisGitTag   = "8.4.2"
)
```

把 Docker 镜像和 git tag 双锁在同一个文件，规范要求两者必须是同一补丁版本，
防止隐式漂移。升级需要显式 PR。

**`manifest.yaml`**：治理台账。每条记录标注文件、状态和理由：

```yaml
- {file: tests/unit/type/string.tcl, status: pass}
- {file: tests/unit/expire.tcl,      status: pass}
- {file: tests/unit/hyperloglog.tcl, status: excluded, reason: "HyperLogLog not yet implemented"}
- {file: tests/unit/scripting.tcl,   status: partial,  reason: "FUNCTION/LDB out of M10 scope",
   skips: ["FUNCTION/*", "LDB/*"]}
```

`partial` 状态意味着「文件会运行，但已知失败的子集被 skip 掉」，比 `excluded`
更诚实——承认部分能力，不是全部放弃。

**`run_tcl.sh`**：克隆固定版本 redis/redis，把 hayakv 伪装成 `redis-server`
（通过 shim 脚本），然后对每个 `pass`/`partial` 文件运行 `test_helper.tcl`，
收集逐文件通过/失败数，写入 TSV 摘要。

**`scoreboard/scoreboard.go` + `history.jsonl`**：每次 nightly 运行追加一行 JSON，
记录通过率、命令数、差分场景数、排除数。趋势可视化，退步立刻可见。

---

## 4 动手验证

以下命令可在仓库根目录直接执行。

**验证覆盖强制**（不需要 Redis，瞬间完成）：

```bash
go test ./test/diff -run TestCorpusMentionsOrExcludesEveryRegisteredCommand -count=1 -v
```

预期输出：

```
=== RUN   TestCorpusMentionsOrExcludesEveryRegisteredCommand
--- PASS: TestCorpusMentionsOrExcludesEveryRegisteredCommand (0.00s)
PASS
ok      github.com/amemiya02/hayakv/test/diff   0.410s
```

这一行通过意味着：所有注册命令都有显式的差分场景或排除理由，没有静默缺席的命令。

**运行差分测试**（需要 Redis 8 或 Docker）：

```bash
# 方式一：有本地 Redis 8
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff -run TestDifferentialRESP2 -count=1

# 方式二：有 Docker（自动拉取 redis:8）
go test ./test/diff -run TestDifferentialRESP2 -count=1

# 方式三：两者都没有（如期 skip）
go test ./test/diff -run TestDifferentialRESP2 -count=1
# ok  github.com/amemiya02/hayakv/test/diff  1.478s
```

无 Redis 且无 Docker 时，测试以 `ok` 退出（全部 skip），而非失败——
skip 是诚实的，不是掩盖问题。CI 在有 Docker 的环境里会真正运行两端比对。

**体验差分抓 bug（练习）**：

想亲身感受差分的报错形态，可以临时修改一条回复文本，再跑差分观察失败：

1. 打开 `internal/command/string.go`，找到 `execSet` 的 `+OK` 回复，临时改为 `+OK2`。
2. 运行 `HAYAKV_DIFF_REDIS_ADDR=... go test ./test/diff -run TestDifferentialRESP2/string_set_get_del -count=1 -v`
3. 观察错误输出：`hayakv: "+OK2\r\n"` vs `redis: "+OK\r\n"`。
4. 改回 `+OK`，测试恢复绿色。

这个过程把「差分报错长什么样」具体化了——不需要真的有 bug，自己制造一个再还原。

---

## 5 延伸阅读

- **`../dev/testing.md`**：hayakv 测试体系全貌——单元测试、集成测试、差分测试
  和 TCL 套件的运行方式、环境变量、CI 配置。本章讲「为什么」，那里讲「怎么用」。
- **`../dev/adding-a-command.md`**：添加一条新命令的完整流程，包括语料库更新
  和排除清单决策——是本章方法论的实操版。
- **Redis 源码 `tests/` 目录**：TCL 套件的原产地。`tests/unit/type/` 下的每个
  文件对应一类数据结构，是 Redis 开发者在实现时同步写下的规范级测试，阅读价值
  不亚于文档。
- **McKeeman, W. M. "Differential Testing for Software." Digital Technical Journal 10.1 (1998)**：
  差分测试方法论的经典文献，奠定了「以两个实现互为 oracle」这一思想的理论基础。
  hayakv 的差分测试体系是这篇论文方法的直接应用。

---

## 收官

至此，「用 Go 重写 Redis」系列走完了十章：从 RESP 帧的 `\r\n` 分隔符，到
Robj 的编码切换阈值，到 dict 的增量 rehash，到 epoll 事件循环，到 AOF 的
多部分重写，到 PSYNC 的增量同步，到 Redis Cluster 的 hash slot 路由，最后
到差分测试这套把「逐字节一致」落进 CI 的方法论。

每一章都在问同一个问题：「真实 Redis 为什么这样设计？」每一章都用 hayakv
的代码作为答案的载体。差分测试章把这个问题推到了元层面：「我们怎么知道自己
的答案是对的？」

方法论是这个系列最大的遗产：oracle + 逐字节 + 覆盖强制 + 归一化，不只适用于
重写 Redis，适用于任何「把既有系统忠实复现一遍」的工程场景。

`docs/learn/README.md` 里列出了若干待写主题（Lua 脚本、Streams、ACL、TLS、
可观测性），那些是下一轮的起点。每一个新主题被实现时，差分语料库里都会新增
场景，排除清单里都会有条目毕业，scoreboard 的通过率会再往上走一格。
**重写从未「完成」——它只是在某一刻的 oracle 面前「暂时通过」。**
