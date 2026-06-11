# 配置参考

> **来源权威性**：本文所有键名、合法值和默认值均以 `config/config.go`（`ServerProperties` 结构体及 `init()` 默认块）为准；`example.conf` 的注释是参考，两者有出入时以代码为准。

---

## 最小可用

hayakv 按以下优先级解析配置：

1. **`CONFIG` 环境变量**——若设置，从该路径读取配置文件：
   ```
   CONFIG=my.conf ./hayakv
   ```
2. **`./redis.conf`**——若当前目录存在，则自动加载。
3. **内置默认值**——无任何配置文件时使用代码中的默认值（`cmd/hayakv/main.go: defaultProperties`）。

此外，命令行参数 `--key value` 可在配置文件基础上叠加覆盖：
```
./hayakv --port 6380 --maxmemory 256mb
```

**最简启动**（随附的 `redis.conf` 已预设，监听 `:6399`）：
```bash
go run ./cmd/hayakv          # 读取 ./redis.conf，监听 0.0.0.0:6399
redis-cli -p 6399 ping       # PONG
```

---

## 配置文件格式

与 redis.conf 一致：每行 `key value`，`#` 开头为注释，键名大小写不敏感。`yes`/`no` 表示布尔值。

---

## GENERAL — 通用

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `bind` | IP 地址 | `127.0.0.1`（代码）/ `0.0.0.0`（example.conf） | 监听地址 |
| `port` | 整数 | `6379`（代码）/ `6399`（example.conf） | TCP 监听端口 |
| `dir` | 路径字符串 | `.`（解析后填充） | 工作目录，AOF/RDB/集群元数据写入此处 |
| `databases` | 整数 | `16` | 逻辑数据库数量（`SELECT 0..N-1`） |
| `maxclients` | 整数 | `1000`（代码） / `128`（example.conf） | 最大同时客户端连接数 |
| `requirepass` | 字符串 | `""` | 若设置，客户端须先 `AUTH` |
| `announce-host` | IP/主机名 | `""` | NAT 后对外公布的地址；空则使用 `bind` |
| `unixsocket` | 文件路径 | `""` | Unix domain socket 路径；若设置，同时启动该监听器 |

> **注意**：`bind` 和 `port` 的默认值在代码 `init()` 中为 `127.0.0.1:6379`，但随附的 `redis.conf`/`example.conf` 将其覆盖为 `0.0.0.0:6399`。实际运行时以加载的配置文件为准。`maxclients` 同理：`cmd/hayakv/main.go` 的 `defaultProperties` 写 `1000`，`example.conf` 写 `128`。

---

## BACKENDS — 后端选择

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `net` | `goroutine` \| `eventloop` | `goroutine` | 网络模型：goroutine-per-conn 或 epoll/kqueue 单线程事件循环 |
| `engine` | `shardmap` \| `redisdb` | `shardmap` | 存储引擎：分片并发 map 或单字典增量 rehash |
| `proto-max` | `resp2` \| `resp3` | `resp2` | 可通过 `HELLO` 协商的最高 RESP 版本；`resp2` 客户端始终可用 |

这三个键切换 seam 实现，详见 [架构详解](architecture.md)。

---

## PERSISTENCE — 持久化

详见[学习文档第 07 章](../learn/07-persistence.md)。

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `appendonly` | `yes` \| `no` | `no` | 启用 AOF 持久化；RDB 快照通过 AOF rewrite 生成，复制也依赖 AOF |
| `appendfilename` | 字符串 | `appendonly.aof` | 单文件 AOF 文件名，相对于 `dir` |
| `appenddirname` | 字符串 | `appendonlydir` | 多段 AOF 目录（Redis 7+ manifest 布局）；存放 base + incr 文件 |
| `appendfsync` | `always` \| `everysec` \| `no` | `""` （解析前无默认） | fsync 策略；example.conf 建议 `everysec` |
| `aof-use-rdb-preamble` | `yes` \| `no` | `yes` | AOF rewrite 时写入 RDB 前缀（混合持久化，Redis 7+ 默认） |
| `dbfilename` | 字符串 | `""` | RDB 文件名，相对于 `dir` |
| `rdb-impl` | `faithful` \| `library` | `library` | RDB 编解码器：`faithful`（内置，字节级对齐真实 Redis）或 `library`（第三方解码器） |

---

## MEMORY / EXPIRY — 内存与过期

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `maxmemory` | 整数字节，或带单位（`100mb`、`1gb`、`500kb` 等） | `0`（不限制） | 内存上限；`0` 表示不限制；支持二进制单位（`kb/mb/gb` = 1024 进制）和十进制单位（`k/m/g` = 1000 进制） |
| `maxmemory-policy` | `noeviction` \| `allkeys-lru` \| `allkeys-lfu` \| `allkeys-random` \| `volatile-lru` \| `volatile-lfu` \| `volatile-random` \| `volatile-ttl` | `noeviction` | 达到 `maxmemory` 后的淘汰策略 |
| `maxmemory-samples` | 整数 | `5` | LRU/LFU 淘汰池采样大小（Redis 默认 5） |
| `hz` | 整数，1–500 | `10` | serverCron 频率（活跃过期扫描等）；每 `1000/hz` ms 执行一次 |

> `maxmemory` 支持单位后缀，通过 `config.parseMemoryBytes` 解析，反射解析器不直接处理字符串后缀，由 `normalizeMemoryConfig` 二次处理（`config/config.go:288`）。

---

## ENCODINGS — 紧凑编码阈值

阈值语义与真实 Redis 8 一致：超过条目数阈值，或向 hash/set/zset 写入超过
`*-max-listpack-value` 长度的单值，即转换为 hashtable/skiplist 等正式结构；
list 没有单值长度维度（与真实 Redis 行为一致）。这些键在启动时生效，也支持
`CONFIG SET` 运行时修改（见下文运行时语义一节）。实现：阈值集中在
`internal/object/thresholds.go`，由 `server.NewStorageEngine` 在启动时从配置
注入；详解见[学习文档第 04 章](../learn/04-datatypes.md)。

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `hash-max-listpack-entries` | 整数 | `128` | Hash 保持 listpack 编码的最大条目数 |
| `hash-max-listpack-value` | 整数（字节） | `64` | Hash listpack 中单个值的最大字节数 |
| `set-max-intset-entries` | 整数 | `512` | Set 保持 intset 编码的最大条目数 |
| `set-max-listpack-entries` | 整数 | `128` | Set 保持 listpack 编码的最大条目数 |
| `set-max-listpack-value` | 整数（字节） | `64` | Set listpack 中单个值的最大字节数 |
| `zset-max-listpack-entries` | 整数 | `128` | Sorted Set 保持 listpack 编码的最大条目数 |
| `zset-max-listpack-value` | 整数（字节） | `64` | Sorted Set listpack 中单个值的最大字节数 |
| `list-max-listpack-size` | 整数 | `128` | List 保持 listpack 编码的最大条目数 |

---

## SLOW LOG — 慢日志

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `slowlog-log-slower-than` | 整数（微秒）；负数禁用；`0` 记录全部 | `10000`（10 ms） | 执行时间超过该值的命令被记录 |
| `slowlog-max-len` | 整数 | `128` | 慢日志环形缓冲区最大条目数（`SLOWLOG RESET` 释放内存） |

---

## REPLICATION — 复制

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `masterauth` | 字符串 | `""` | 主节点的 `requirepass` 密码，副本连接主节点时使用 |
| `repl-timeout` | 整数（秒） | `0` | 副本等待主节点数据的超时秒数 |
| `slave-announce-ip` | IP/主机名 | `""` | NAT 后副本向主节点公布的地址 |
| `slave-announce-port` | 整数 | `0` | NAT 后副本向主节点公布的端口 |
| `repl-backlog-size` | 整数（字节） | `1048576`（1 MB） | PSYNC 部分重同步的环形回放缓冲区大小 |
| `repl-diskless-sync` | `yes` \| `no` | `no` | 磁盘less 复制：RDB 直接流式发送到副本 socket，不落临时文件 |

---

## CLUSTER — 集群

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `cluster-enable` | `yes` \| `no` | `no` | 启用集群模式；以下选项仅在 `yes` 时生效 |
| `cluster-mode` | `redis` \| `""` | `""` | 集群方案：`redis` = 原生 Redis Cluster（CLUSTER MEET / ADDSLOTS，MOVED/ASK，gossip bus on port+10000）；空或未设置 = Raft 代理集群（对客户端透明） |
| `cluster-config-file` | 文件名 | `nodes.conf` | Redis Cluster 节点状态文件（相对于 `dir`，首次启动创建，自动更新） |
| `cluster-node-timeout` | 整数（毫秒） | `15000` | 节点故障检测超时（Redis Cluster） |
| `cluster-as-seed` | `yes` \| `no` | `no` | 引导新 Raft 集群；磁盘上已有集群数据时不生效；重启后自动重新加入 |
| `cluster-seed` | `host:port` | `""` | 通过已有成员加入 Raft 集群（redis 服务地址，非 raft 地址，仅首次加入时使用） |
| `master-in-cluster` | `host:port` | `""` | 以指定 Raft 成员的副本身份加入集群（redis 服务地址） |
| `raft-listen-address` | `host:port` | `""` | Raft 协议监听地址 |
| `raft-advertise-address` | `host:port` | `""` | NAT 后对外公布的 Raft 地址 |

---

## 其他键（parser 支持，example.conf 中未列出）

以下键在 `ServerProperties` 结构体中有定义，但 `example.conf` 未提及，通过命令行参数 `--key value` 或自定义配置文件可使用：

| 键 | 取值 | 默认 | 作用 |
|---|---|---|---|
| `announce-host` | 主机名/IP | `""` | 节点对外公布的 redis 服务地址（主机部分），用于 `AnnounceAddress()` |
| `unixsocket` | 文件路径 | `""` | Unix domain socket 监听路径（与 TCP 同时启动） |
| `busy-reply-threshold` | 整数（毫秒） | `0` | Lua 脚本超时阈值（`BUSY` 回复触发点），`0` = 使用代码内置逻辑 |
| `notify-keyspace-events` | 标志字符串，如 `KEA`、`Elg` | `""` | 键空间通知事件掩码；空字符串表示禁用 |
| `latency-monitor-threshold` | 整数（毫秒） | `0` | 延迟监控阈值；`0` = 禁用 |
| `aclfile` | 文件路径 | `""` | ACL 规则文件路径 |
| `tls-port` | 整数 | `0` | TLS 监听端口；`0` = 不启用 TLS |
| `tls-cert-file` | 文件路径 | `""` | TLS 服务端证书文件 |
| `tls-key-file` | 文件路径 | `""` | TLS 服务端私钥文件 |
| `tls-ca-cert-file` | 文件路径 | `""` | TLS CA 证书文件（用于客户端证书验证） |
| `tls-replication` | `yes` \| `no` | `no` | 复制链路启用 TLS |

---

## CONFIG GET / SET 运行时语义

hayakv 支持 `CONFIG GET`、`CONFIG SET`、`CONFIG REWRITE` 和 `CONFIG RESETSTAT`（`internal/command/config_cmd.go`）。

### 可在运行时动态修改的键（CONFIG SET）

以下键支持 `CONFIG SET`，修改立即生效，无需重启（`config_cmd.go:108-190`）：

| 键 | 备注 |
|---|---|
| `maxmemory` | 支持单位后缀（`100mb` 等），调用 `config.ParseMemoryBytes` 解析 |
| `maxmemory-policy` | 仅接受合法策略名，大小写不敏感 |
| `maxmemory-samples` | 须为正整数 |
| `hz` | 须为正整数 |
| `appendonly` | 接受 `yes`/`1` 或 `no`/`0` |
| `notify-keyspace-events` | 标志字符串 |
| `slowlog-log-slower-than` | 负数禁用；同步更新内存中的 slowlog 阈值 |
| `slowlog-max-len` | 须为正整数；同步更新 slowlog 缓冲区容量 |
| `hash-max-listpack-entries` 等 8 个编码阈值键 | 须为正整数；修改即时作用于后续写入（已转换 key 的编码不会回退），经 `ApplyEncodingThresholds` 推入 object 包 |

**CONFIG GET** 支持 glob 通配符（如 `CONFIG GET maxmemory*`、`CONFIG GET *`），可查询的键集合在 `configGet` 函数中硬编码（`config_cmd.go:51-77`），含全部 8 个编码阈值键。

**启动时专有键**：`net`、`engine`、`proto-max`、`bind`、`port`、`databases`、`cluster-*`、`tls-*`、`unixsocket`、`dir` 等不支持 `CONFIG SET`——修改须编辑配置文件后重启。未知键的 `CONFIG SET` 请求会被静默接受并返回 `OK`（向 Redis 兼容性对齐）。

---

## 参考

带注释的完整配置示例见项目根目录的 [`example.conf`](../../example.conf)，每个分组均有详细说明。
