# 为 hayakv 做贡献

hayakv 是一个学习项目，优先级依次为：正确性 → 可读性 → 性能。
验收标准是与真实 Redis 8.x 逐字节回复一致。

## 开发环境

Go 1.24+。`go build ./cmd/hayakv` 即可构建，无其它依赖；
跑差分测试需要 Docker 或一个现成的 Redis 8 实例。

## 提交前检查

    gofmt -l $(find . -name '*.go' -type f)   # 必须无输出
    go vet ./...
    go test -race ./...
    go test ./test/integration -count=1
    HAYAKV_DIFF_REDIS_ADDR=127.0.0.1:6379 go test ./test/diff   # 或由 Docker 自动拉起

## commit 规范

conventional commits：`feat:` / `fix:` / `docs:` / `chore:` / `perf:` / `ci:`，
可带作用域，如 `fix(tls): ...`、`docs(learn): ...`。一个 commit 做一件事。

## 加命令必读

新增命令 handler 后，差分语料的覆盖检查
（`test/diff/coverage_test.go` 中的 `TestCorpusMentionsOrExcludesEveryRegisteredCommand`）
会强制要求：要么在语料中覆盖该命令，要么显式加入排除清单。
完整流程见 [docs/dev/adding-a-command.md](docs/dev/adding-a-command.md)。

## 不在范围内

Redis 模块（JSON、search/query、TimeSeries、概率型结构）。

## 许可

GPL-3.0（fork 自 HDT3213/godis，请保留 LICENSE 归属）。
