# 第 01 章：RESP2/RESP3 协议与 HELLO 协商

> **系列**：用 Go 重写 Redis — hayakv 代码导读

---

## 1. 本章导读

本章讲 Redis 的有线协议（wire protocol）：客户端和服务端通信所用的编码格式。
理解协议是读懂 Redis 任何其他部分的前提，因为所有命令都经由它进出服务端。

**你将学到：**

- RESP 是一种什么样的协议设计，为什么选它；
- RESP2 的五种消息类型及其在线格式；
- RESP3 新增了哪些类型，解决了什么问题；
- `HELLO` 命令如何按连接协商版本（per-connection 状态）；
- hayakv 怎样用 Go interface 把协议层抽象成可替换的"接缝"，
  以及 RESP2/RESP3 两套实现各自做什么、共享什么。

**前置知识：** 会使用 `redis-cli` 即可，不需要读过其他章节。

---

## 2. Redis 原理

### 2.1 为什么是前缀长度协议

Redis 最初为速度而生，协议设计极简：**每条消息先告诉你它有多长（或有几个元素），
再给出数据本身**。这种"前缀长度"（length-prefix）风格有两个好处：

1. **无需扫描分隔符**。解析器读到长度后直接 `ReadFull(n bytes)`，O(1) 定位到消息末尾，
   不必逐字节扫描寻找转义或边界。
2. **可流式处理**。TCP 是字节流；有了长度前缀，接收方在数据还在路上的时候就能
   知道"这条消息需要再等多少字节"，缓冲区管理简单且精确。

消息之间用 `\r\n`（CRLF）分隔行，因此协议对人类也基本可读——这是 Redis 调试
体验好的重要原因之一。

> **Inline command**：redis-cli 有时会发送不带 `*` 前缀的纯文本行（如 `PING\r\n`），
> 服务端遇到非 `*` 起始字节时按空格拆分并当作数组处理。这是对老客户端的兼容后门，
> 实际生产中几乎不用。

### 2.2 RESP2 的五种类型

RESP2（Redis Serialization Protocol 版本 2）由首字节决定类型，之后紧跟数据。

| 首字节 | 类型 | 线格式示例 |
|---|---|---|
| `+` | Simple String（简单字符串） | `+OK\r\n` |
| `-` | Error（错误） | `-ERR wrong type\r\n` |
| `:` | Integer（整数） | `:42\r\n` |
| `$` | Bulk String（二进制安全字符串） | `$5\r\nhello\r\n` |
| `*` | Array（数组） | `*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n` |

`$-1\r\n` 表示 null bulk string；`*-1\r\n` 表示 null array——两种 null 形式
是 RESP2 的历史包袱，RESP3 统一用单一的 null 类型替代。

### 2.3 RESP3 新增类型

Redis 6.0 引入 RESP3，目标是让回复**携带类型信息**，客户端不再需要靠命令名
猜测"这个数组其实是一张 map"。RESP3 新增的主要类型：

| 首字节 | 类型 | 线格式示例 |
|---|---|---|
| `_` | Null | `_\r\n` |
| `#` | Boolean | `#t\r\n` / `#f\r\n` |
| `,` | Double | `,3.14\r\n` |
| `(` | Big number | `(12345678901234567890\r\n` |
| `%` | Map | `%2\r\n$3\r\nfoo\r\n:1\r\n$3\r\nbar\r\n:2\r\n` |
| `~` | Set | `~3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n` |
| `>` | Push（服务端主动推送） | `>3\r\n$9\r\nsubscribe\r\n...` |
| `=` | Verbatim string（带格式标签） | `=10\r\ntxt:hello\r\n` |

`%` 开头的 map 格式：`%N\r\n` 后跟 N 对 key-value 帧，每帧本身是合法的 RESP3 值。

### 2.4 HELLO 协商：per-connection 版本状态

RESP3 采用**逐连接协商**（per-connection）而非全局配置——同一服务端可以同时
接受 RESP2 和 RESP3 客户端，互不干扰。协商由 `HELLO` 命令完成：

```
HELLO [protover [AUTH user pass] [SETNAME name]]
```

- `HELLO`（无参数）：返回当前连接的握手信息，不改变版本；
- `HELLO 3`：切换当前连接到 RESP3，返回服务端握手 map；
- `HELLO 2`：切回 RESP2（可降级）；
- `HELLO 4`（不存在的版本）：返回 `NOPROTO` 错误，连接版本不变。

版本状态是**连接对象的成员**，不是全局变量。hayakv 的配置项 `proto-max`
设置服务端支持的版本**上限**（`resp2` 或 `resp3`），`HELLO 3` 只有在
`proto-max = resp3` 时才能成功。

---

## 3. hayakv 实现带读

### 3.1 ProtocolCodec 接缝

hayakv 用 Go interface 把协议层与命令层彻底解耦。接缝定义在
`internal/iface/seams.go`（第 43–47 行）：

```go
// ProtocolCodec is the seam between wire bytes and domain replies.
type ProtocolCodec interface {
    DecodeStream(reader io.Reader) <-chan ProtocolPayload
    DecodeOne(data []byte) (iredis.Reply, error)
    Encode(reply iredis.Reply, resp RespVersion) []byte
}
```

三个方法职责清晰：

- `DecodeStream`：把字节流（`io.Reader`）转成 `ProtocolPayload` channel，
  适合长连接逐帧读取；
- `DecodeOne`：解析单条报文，供复制等一次性场景使用；
- `Encode`：把服务端 `Reply` 序列化为字节，**须传入版本号**，
  因为同一个 `Reply` 在 RESP2/RESP3 下的字节表示可能不同。

### 3.2 resp2 解码：parse0 逐帧状态机

RESP2 解码器的核心是 `internal/proto/resp2/parser/parser.go` 中的
`parse0()` 函数（第 63 行）。它用一个 `bufio.Reader` 逐行读取，
靠**首字节 switch** 决定后续动作：

```
+  → 读到 CRLF，发出 StatusReply
-  → 读到 CRLF，发出 ErrReply
:  → 读到 CRLF，ParseInt，发出 IntReply
$  → 读长度 N，ReadFull(N+2 字节)，发出 BulkReply      ← parseBulkString (第 134 行)
*  → 读元素数 M，循环读 M 个 bulk string，发出 MultiBulkReply ← parseArray (第 214 行)
其他 → 按空格拆分，inline command
```

`parseBulkString` 处理 `$-1\r\n`（null bulk）和普通 bulk 两种情况：读到长度后
立即 `io.ReadFull`，**不逐字节扫描**，直接跳到数据末尾，这就是前缀长度设计
高效解析的具体体现。

解析结果通过 channel 发出而非直接 return，原因是 `parseArray` 是递归逐元素
读取的——用 channel 使调用方可以边读边处理，也更容易在单元测试中断言每一帧。

### 3.3 resp2 编码：ToBytes 委托模型

RESP2 `Codec.Encode`（`internal/proto/resp2/codec.go` 第 37–41 行）：

```go
func (Codec) Encode(reply iredis.Reply, _ iface.RespVersion) []byte {
    if reply == nil {
        return nil
    }
    return reply.ToBytes()
}
```

注意第二个参数用 `_` 忽略——**RESP2 编码器不区分协议版本**，永远调用
`reply.ToBytes()`，由每种 Reply 类型自行负责序列化。这是一个简洁的委托模型。

### 3.4 resp3 编码：版本感知的 EncodeRESP3

RESP3 `Codec.Encode`（`internal/proto/resp3/codec.go` 第 33–41 行）在版本为
`RESP3` 时调用 `EncodeRESP3(reply)`，否则退化为 `reply.ToBytes()`（RESP2 兼容）：

```go
func (Codec) Encode(reply iredis.Reply, resp iface.RespVersion) []byte {
    if reply == nil {
        return nil
    }
    if resp == iface.RESP3 {
        return EncodeRESP3(reply)
    }
    return reply.ToBytes()
}
```

`EncodeRESP3`（`internal/proto/resp3/encode.go` 第 11–19 行）的策略是：
**RESP3 原生类型自己序列化，遗留 RESP2 类型走兼容转换**：

```go
func EncodeRESP3(reply iredis.Reply) []byte {
    switch reply.(type) {
    case *NullReply, *BoolReply, *DoubleReply, *BigNumberReply,
        *MapReply, *SetReply, *PushReply, *VerbatimReply:
        return reply.ToBytes()
    default:
        return EncodeRESP3FromRESP2(reply.ToBytes())
    }
}
```

`EncodeRESP3FromRESP2`（同文件第 22–28 行）只做一件事：
把 RESP2 的两种 null 形式（`$-1\r\n`、`*-1\r\n`）改写为 RESP3 的统一 null `_\r\n`，
其他所有 RESP2 帧直接透传——因为 RESP2 帧在 RESP3 规范里是合法子集。

### 3.5 RESP3 类型的 Go 实现

`internal/proto/resp3/reply.go` 定义了所有 RESP3 原生类型。以 Map 为例
（第 56–66 行）：

```go
type MapReply struct{ Pairs []iredis.Reply }

func (r *MapReply) ToBytes() []byte {
    var b bytes.Buffer
    b.WriteString("%" + strconv.Itoa(len(r.Pairs)/2) + CRLF)
    for _, p := range r.Pairs {
        b.Write(p.ToBytes())
    }
    return b.Bytes()
}
```

`Pairs` 是偶数长度的 slice（`[key0, val0, key1, val1, ...]`），`ToBytes()` 先写
`%N\r\n`（N = Pairs 长度 / 2），再依次序列化每个元素。

### 3.6 HELLO 的处理路径

`HELLO` 在命令层有专属快速路径。`internal/command/server.go`（第 191–192 行）
在常规命令 dispatch 之前拦截：

```go
if cmdName == "hello" {
    return execHello(c, cmdLine[1:])
}
```

`execHello`（`internal/command/hello.go` 第 15–52 行）解析协议版本号，
调用 `conn.SetProtocol(redis.RespVersion(ver))` 把版本写入连接对象，
再调用 `helloReply(conn)` 构造回复。

`helloReply`（第 54–68 行）根据当前连接版本选择回复类型：

```go
if conn.Protocol() == redis.RESP3 {
    return resp3.MakeMapReply(pairs)
}
return protocol.MakeMultiRawReply(pairs)
```

**RESP3 连接收到 `%7\r\n...` 的 map；RESP2 连接收到 `*14\r\n...` 的扁平数组**——
相同内容，不同结构，这正是 RESP3 的核心价值。

### 3.7 两协议下的回复差异：HGETALL 真实案例

差分语料 `test/diff/corpus_resp3_test.go`（第 9–13 行）验证了 HGETALL 在
RESP3 下返回 map：

```go
{Name: "resp3 hgetall is map", Commands: []Command{
    {Args: []string{"HSET", "h", "f1", "v1", "f2", "v2"}},
    {Args: []string{"HGETALL", "h"}},
}},
```

HGETALL 的 RESP2 回复是扁平数组 `*4\r\n$2\r\nf1\r\n$2\r\nv1\r\n...`，
客户端需要自己知道"这是个 hash，两个元素一对"才能还原结构；
RESP3 回复是 `%2\r\n$2\r\nf1\r\n$2\r\nv1\r\n...`，map 结构直接编码在线格式中。

---

## 4. 动手验证

从仓库根目录依次执行（需要已安装 `redis-cli` 和 `nc`）：

### 步骤一：构建并启动服务端

```bash
go build -o hayakv ./cmd/hayakv && ./hayakv &
```

### 实验 1：裸 RESP 帧——PING

```bash
printf '*1\r\n$4\r\nPING\r\n' | nc -w1 localhost 6399
```

实际输出：

```
+PONG
```

手工构造了一条 RESP array 帧（`*1`：一个元素，`$4\r\nPING`：4 字节 bulk），
服务端回复 simple string `+PONG`。这证明协议是纯文本可构造的。

### 实验 2：HELLO 3 握手

```bash
redis-cli -3 -p 6399 hello 3
```

实际输出：

```
server hayakv
version 8.0.0
proto 3
id 1
mode standalone
role master
modules 
```

redis-cli 把 map 展开显示。握手字段中 `proto 3` 确认协议版本已切换到 RESP3。

### 实验 3：CONFIG GET（RESP2 — 扁平数组）

```bash
redis-cli -p 6399 config get maxmemory
```

实际输出：

```
maxmemory
0
```

在 RESP2 连接中，`CONFIG GET` 返回 `*2\r\n$9\r\nmaxmemory\r\n$1\r\n0\r\n`——
两个元素的扁平数组，key 与 value 交替排列，客户端要自己识别配对关系。

### 实验 4：CONFIG GET（RESP3 — 当前行为）

```bash
redis-cli -3 -p 6399 config get maxmemory
```

实际输出：

```
maxmemory
0
```

用原始字节查看：

```bash
printf '*2\r\n$5\r\nHELLO\r\n$1\r\n3\r\n*3\r\n$6\r\nCONFIG\r\n$3\r\nGET\r\n$9\r\nmaxmemory\r\n' \
  | nc -w1 localhost 6399 | cat -v
```

HELLO 握手之后，`CONFIG GET` 的回复仍是 `*2\r\n...`（RESP2 格式），
而不是 RESP3 map。这是 hayakv 当前的局限：command handler 返回的是
`MultiBulkReply`，`EncodeRESP3FromRESP2` 透传 RESP2 格式，只有命令
handler 主动返回 `MapReply` 才能升级为 map（`HGETALL` 和 `HELLO` 已这样实现）。

> **与真实 Redis 的差异**：真实 Redis 8 在 RESP3 连接上，`CONFIG GET` 返回
> `%1\r\n` 开头的 map 帧。这是个值得贡献的改进点。

### 步骤二：清理

```bash
kill %1 2>/dev/null; rm -f hayakv dump.rdb; rm -rf appendonlydir
```

---

## 5. 延伸阅读

- **RESP 协议规范**（官方）：<https://redis.io/docs/latest/develop/reference/protocol-spec/>
- **差分语料（RESP3 场景）**：[`../../test/diff/corpus_resp3_test.go`](../../test/diff/corpus_resp3_test.go) — 用于逐字节验证 RESP3 回复与真实 Redis 一致的测试用例
- **架构总览**：[`../dev/architecture.md`](../dev/architecture.md) — 各接缝（seam）的工厂配置与运行时选择逻辑
- **测试体系**：[`../dev/testing.md`](../dev/testing.md) — 差分测试、集成测试的运行方式

---

*本章覆盖 `internal/proto/`、`internal/iface/seams.go`、`internal/command/hello.go`
五个文件，约 400 行代码，是 hayakv 最薄也最基础的一层。*
