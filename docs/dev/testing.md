# 测试体系

hayakv 的测试分四层，每层解决不同的置信问题。本文是这四层的定位说明与操作手册，面向决定"我该跑哪些测试、添加什么测试"的贡献者。如果你在添加新命令，§4 的覆盖强制机制与 [adding-a-command.md §4–§5](adding-a-command.md) 配套阅读。

---

## 1. 四层金字塔总览

| 层 | 位置 | 一条跑法命令 | 什么时候需要加这层的测试 |
|---|---|---|---|
| ① 单元测试 + race | 与被测文件同包同目录 | `go test -race ./...` | 新增或修改任何函数逻辑时 |
| ② 集成测试 | `test/integration/` | `go test -count=1 ./test/integration` | 跨进程、多连接、TLS、复制等场景 |
| ③ 差分测试（验收门） | `test/diff/` | `go test -count=1 ./test/diff -run TestDifferentialRESP2` | 新增命令或修改回复格式时（**必须**） |
| ④ TCL 脚手架（可选） | `test/tcl/` | `bash test/tcl/run_tcl.sh` | 对应的上游 Redis TCL 文件已在 manifest 中标记 pass 时 |

**当前数量基线**：仓库中共有 **288** 个 `_test.go` 文件（`find . -name '*_test.go' | wc -l`），其中单元测试广泛分布于 `internal/` 下各子包，差分语料集中在 `test/diff/`，集成测试在 `test/integration/`。

---

## 2. 差分 harness 详解

差分测试是 hayakv **真正的验收门**：每条命令的回复都与真实 Redis 8 逐字节比对。下面逐一拆解其机制。

### 2.1 语料即 Go 测试文件

`test/diff/` 下的 17 个 `corpus_*_test.go` 按命令域拆分，各自独立，互不耦合：

| 文件 | 覆盖域 |
|---|---|
| `corpus_base_test.go` | 字符串、列表、哈希、集合、有序集合等核心命令（主语料，由 `TestDifferentialRESP2` 驱动） |
| `corpus_8x_test.go` | Redis 8.x 新语义（BITOP DIFF/AND/OR/NOT 等）；由 `TestDifferential8x` 驱动 |
| `corpus_auth_test.go` | AUTH / requirepass 场景；由 `TestDifferentialAuth` 驱动 |
| `corpus_census_test.go` | 数量统计类命令；由 `TestDifferentialCensus` 驱动 |
| `corpus_cluster_test.go` | 集群相关命令的单机回复语义 |
| `corpus_encoding_test.go` | OBJECT ENCODING 回复；由 `TestObjectEncoding*` 系列驱动 |
| `corpus_eval_test.go` | EVAL / Lua 脚本；由 `TestDifferentialEval` 驱动 |
| `corpus_expiry_test.go` | EXPIRE / TTL / PERSIST / EXPIRETIME 等过期命令；由 `TestDifferentialExpiry` 驱动 |
| `corpus_geo_test.go` | GEO* 命令；由 `TestDifferentialGeo` 驱动 |
| `corpus_hashttl_test.go` | Hash TTL（HEXPIRE / HTTL 等 Redis 7.4+ 扩展）；由 `TestDifferentialHashTTL` 驱动 |
| `corpus_keyspace_test.go` | TYPE / RENAME / COPY / OBJECT 等键空间命令；由 `TestDifferentialKeyspace` 驱动 |
| `corpus_pubsub_test.go` | PUBLISH / SUBSCRIBE（多连接场景）；由 `TestDifferentialPubSub` 驱动 |
| `corpus_redisdb_test.go` | `engine=redisdb` 后端专属场景；由 `TestDifferentialRedisDB` 驱动 |
| `corpus_resp3_test.go` | RESP3 协议帧格式；由 `TestDifferentialRESP3` 驱动 |
| `corpus_scan_test.go` | SCAN / HSCAN / SSCAN / ZSCAN；由 `TestDifferentialScan` 驱动 |
| `corpus_txn_test.go` | MULTI / EXEC / DISCARD / WATCH；由 `TestDifferentialTxn` 驱动 |
| `corpus_variants_test.go` | 参数变体（NX/XX/GT/LT/KEEPTTL 等选项组合）；由 `TestDifferentialVariants` 驱动 |

每个语料文件暴露一个 `xxxCorpus()` 函数，返回 `[]Scenario`；每个 `Scenario` 包含若干 `Command`（`Args` + 可选的 `Normalize` 钩子）。harness 在执行每个 Scenario 前自动发送 `FLUSHALL` 保证隔离（`harness_test.go: runScenario`）。

### 2.2 真实 Redis 的三级来源

`startRedis8` 函数（`test/diff/harness_test.go`）按优先级依次尝试三个来源：

**第一级 — 环境变量（速度最快）**

```bash
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff -count=1
```

设置 `HAYAKV_DIFF_REDIS_ADDR` 后，harness 直接连接该地址，不启动任何容器。CI 正是用这种方式——`ci.yml` 通过 GitHub Actions service 起 `redis:8.4.2`，然后注入 `HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379`。

**第二级 — Docker 自动拉起（本地开发的默认路径）**

若未设置环境变量，harness 检测 `docker` 是否在 PATH 中且 Docker daemon 可达，若满足则自动执行：

```
docker run --rm --name hayakv-redis8-<nano> -p <port>:6379 redis:8 redis-server --save "" --appendonly no
```

容器在测试结束后自动销毁（`docker rm -f`）。注意版本差异：harness 拉取的是**浮动标签** `redis:8`（跟随最新 8.x patch），而 CI 的 service 容器钉死在 `redis:8.4.2`；`test/tcl/redisversion.go` 中的 `RedisImageTag = "redis:8.4.2"` 只约束 TCL 运行器（见 §4），与差分 harness 无关。本地差分结果若与 CI 不一致，先核对两边 Redis 小版本。

**第三级 — 干净 skip**

若 Docker 也不可用，测试干净跳过，不报 FAIL：

```
=== RUN   TestDifferentialRESP2
    harness_test.go:246: docker daemon not reachable; set HAYAKV_DIFF_REDIS_ADDR or start Docker
--- SKIP: TestDifferentialRESP2 (1.79s)
PASS
ok  	github.com/amemiya02/hayakv/test/diff	2.191s
```

这是在无 Docker 的 CI 节点或纯离线环境下的预期行为。

### 2.3 逐字节比较意味着什么

`assertReplyEqual`（`harness_test.go`）对每条命令的回复做 `bytes.Equal`。在应用 `Normalize` 钩子之后，hayakv 的回复必须与 Redis 8 的回复**一个字节不差**。

这意味着**凡输出非确定的命令，必须先挂归一化钩子才能进语料**，否则测试会随机失败：

- `TTL` / `PTTL` — 近同时执行时两边剩余时间可能差 1。`corpus_8x_test.go` 中的 `normalizeTTL` 将正整数结果归一到 `":1\r\n"`，使比较稳定：

  ```go
  // normalizeTTL clamps a positive integer reply to "1" so that
  // near-simultaneous TTL values don't cause false diff failures.
  func normalizeTTL(raw []byte) []byte { ... }

  // 用法：
  {Args: []string{"TTL", "x"}, Normalize: normalizeTTL},
  ```

- `SCAN` 游标 — `normalizeScan`（`corpus_base_test.go`）将游标值替换为占位符，只比较元素集合。

- `INFO` / `TIME` / 随机命令 — 当前列在 `diffExclusions` 中，尚未进入语料；若未来要加，需先实现对应的归一化钩子。

### 2.4 可跑的语料测试名完整列表

以下均位于 `test/diff/` 包，可用 `-run <TestName>` 单独执行：

```
TestDifferentialRESP2          # 主语料（baseCorpus）
TestDifferentialRESP3          # RESP3 帧
TestDifferentialRedisDB        # redisdb 后端
TestDifferentialExpiry         # 过期命令
TestDifferential8x             # Redis 8.x 新语义
TestDifferentialAuth           # AUTH
TestDifferentialCensus         # 数量统计
TestDifferentialEval           # Lua EVAL
TestDifferentialGeo            # GEO*
TestDifferentialHashTTL        # Hash TTL
TestDifferentialKeyspace       # 键空间
TestDifferentialPubSub         # 发布/订阅
TestDifferentialScan           # SCAN 系列
TestDifferentialTxn            # 事务
TestDifferentialVariants       # 参数变体
TestObjectEncodingDiff         # OBJECT ENCODING（差分对比）
TestObjectEncodingHash         # Hash 编码
TestObjectEncodingHayakv       # hayakv 独立断言
TestObjectEncodingList         # List 编码
TestObjectEncodingNonExistent  # 不存在键
TestObjectEncodingSet          # Set 编码
TestObjectEncodingSortedSet    # ZSet 编码
TestRDBCrossLoadHayakvToRedis  # RDB 跨加载
TestRDBCrossLoadRedisToHayakv  # RDB 跨加载（反向）
TestCorpusMentionsOrExcludesEveryRegisteredCommand  # 覆盖强制（见 §3）
```

典型本地跑法：

```bash
# 单语料
go test ./test/diff -run TestDifferentialRESP2 -count=1

# 全部差分（需要 Redis 8 可达）
HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff -count=1
```

---

## 3. 覆盖强制：`TestCorpusMentionsOrExcludesEveryRegisteredCommand`

`test/diff/coverage_test.go` 中的这个测试在 CI 中**无需 Redis 即可运行**（它不对比回复，只检查命令名称的覆盖状态）。它的逻辑是：

1. 调用 `database.RegisteredCommandNames()` 拿到所有已注册命令的名称。
2. 扫描 10 个主体语料函数（`baseCorpus`、`txnCorpus`、`scanCorpus` 等）中出现的每个 `Args[0]`，建立 `covered` 集合。
3. 对于每条已注册但不在 `covered` 集合中的命令，检查 `diffExclusions` 表是否有对应条目。
4. 若既不被覆盖、也无排除理由，测试立即 `t.Fatalf`，**CI 失败**。

**`diffExclusions` 的维护规则**已在 [adding-a-command.md §4](adding-a-command.md#4-差分语料是强制的) 中详细说明，本文不重复，仅列几类典型排除理由供参考：

- **非确定性**（`info`、`randomkey`）：需先实现归一化钩子。
- **阻塞命令**（`blpop`、`brpop`）：超时语义不可逐字节对比。
- **godis 内部命令**（`copyfrom`、`dumpkey` 等）：非公开 Redis 命令，本来就不应出现在语料中。
- **多连接命令**（`publish`、`subscribe`）：已在 `corpus_pubsub_test.go` 中用 `runScenarioMultiConn` 单独覆盖。
- **暂缓语料**（Streams、HyperLogLog、BITFIELD）：实现已有但差分语料尚在开发中，注释写明原因。

---

## 4. TCL 脚手架

`test/tcl/` 是"渐进收编 Redis 官方 TCL 测试"的脚手架，当前定位为**可选**——CI 不跑它，贡献者在本地验证对 upstream 的兼容程度时使用。

### 4.1 三个核心文件

**`manifest.yaml`** — 治理台账，逐文件声明 upstream `tests/` 目录中每个文件的状态：
- `pass`：预期全部通过，runner 会执行它。
- `partial`：有若干已知失败项（`skips` 字段列出），runner 带 `--skip` 执行。
- `excluded`：明确排除，附 `reason`。

当前标记为 `pass` 或 `partial` 的文件覆盖 `tests/unit/type/`（string、incr、list、hash、set、zset）以及 `tests/unit/expire.tcl`、`tests/unit/info.tcl`、`tests/unit/keyspace.tcl` 等共约 20 个文件；Streams、HyperLogLog、pub/sub 等尚未就绪的域标记为 `excluded`。

**`redisversion.go`** — 版本锁定常量，同时约束 Docker 镜像和 Git tag：

```go
const (
    RedisImageTag = "redis:8.4.2"
    RedisGitTag   = "8.4.2"
)
```

两个常量**必须同步修改**，否则 `manifest_test.go` 的覆盖门禁会失败。

**`run_tcl.sh`** — 端到端运行脚本，执行流程如下：

1. 从 `redisversion.go` 解析 `RedisGitTag`（或读取 `REDIS_GIT_TAG` 环境变量），`git clone --depth 1` 对应 tag 的 `redis/redis` 仓库到临时目录（若 `REDIS_TCL_DIR` 已设则直接用）。
2. `go build` 生成 hayakv 二进制。
3. 创建 `redis-server` shim 脚本，`exec` 到 hayakv，让 TCL 测试框架无感知地使用 hayakv 代替真实 Redis。
4. 解析 `manifest.yaml`，依次对所有 `pass`/`partial` 文件调用 `tclsh test_helper.tcl --single <file>`。
5. 汇总每个文件的通过/失败数，打印 scoreboard 并在有失败时以非零退出码退出。

### 4.2 本地运行

```bash
# 运行全部 in-scope 文件（首次执行会克隆 redis/redis，约需几分钟）
bash test/tcl/run_tcl.sh

# 只跑一个文件
bash test/tcl/run_tcl.sh tests/unit/type/string.tcl

# 使用已有的 redis 源码目录，跳过克隆
REDIS_TCL_DIR=/path/to/redis/tests bash test/tcl/run_tcl.sh
```

**`test/tcl/scoreboard/`** 下的 `scoreboard.go` / `history.jsonl` 用于记录历史通过率，供长期趋势跟踪。

---

## 5. CI 门禁

CI 配置位于 `.github/workflows/ci.yml`，触发条件：`push` 到 `main`、任何 Pull Request、手动 `workflow_dispatch`。

所有 job 运行在 `ubuntu-latest`，并通过 GitHub Actions service 自动起 `redis:8.4.2`：

```yaml
services:
  redis8:
    image: redis:8.4.2
    ports:
      - 6379:6379
    options: >-
      --health-cmd "redis-cli ping"
      --health-interval 2s
      --health-timeout 2s
      --health-retries 30
```

差分测试使用 `HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379`。

以下是 CI 实际执行的全部 step（以文件内容为准）：

| Step | 命令 | 说明 |
|---|---|---|
| Install redis-cli | `sudo apt-get install -y redis-tools` | 集成测试需要 redis-cli |
| Download dependencies | `go mod download` | 预热 Go module 缓存 |
| **Verify formatting** | `test -z "$(gofmt -l $(find . -name '*.go' -type f))"` | 格式不合规直接失败 |
| **Vet** | `go vet ./...` | 静态分析 |
| **Race tests** | `go test -race -count=1 ./config ./internal/datastruct/... ./internal/proto/... ./internal/net/goroutine ./internal/net/eventloop ./internal/server ./internal/iface ./cmd/hayakv -timeout 10m` | 大多数包的竞态检测 |
| **Command race tests** | `go test -race -count=1 ./internal/command -timeout 10m`（`GOMEMLIMIT=5GiB`） | 命令层单独跑，内存限制 5 GiB |
| **Integration tests** | `go test -count=1 ./test/integration` | 全集成测试 |
| **Replication tests** | `go test -count=1 -p 1 ./test/integration -run 'TestReplica\|TestReplconf\|...'` | 复制专项，串行执行（`-p 1`） |
| **Differential tests** | `go test -count=1 ./test/diff -run TestDifferentialRESP2`<br>`go test -count=1 ./test/diff -run TestDifferentialRedisDB`<br>`go test -count=1 ./test/diff -run TestDifferential8x` | 三个主差分语料 |
| **Build** | `go build ./cmd/hayakv` | 最终构建验证 |

> **注意**：`gofmt` 和 `go vet` 在本地提交前应主动运行，格式问题会直接阻断 CI。

---

## 6. 快速决策：我该加哪层测试？

| 场景 | 需要加的测试 |
|---|---|
| 修改了一个内部函数 | ① 单元测试（`*_test.go` 同包） |
| 新增了一条命令 | ① 单元测试 + **③ 差分语料**（必须） |
| 修改了回复格式 | ③ 差分语料更新 |
| 多连接 / 复制 / TLS 场景 | ② 集成测试 |
| 对应 upstream TCL 文件已标记 pass | ④ 确认本地 TCL 通过后提 PR |
| 新增非确定性命令（随机/时间相关） | 先写 `Normalize` 钩子，再加 ③ 差分语料 |

相关文档：[架构详解](architecture.md) · [如何添加一个命令](adding-a-command.md)
