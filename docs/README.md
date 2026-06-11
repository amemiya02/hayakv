# hayakv 文档

两条阅读路线：

- **我要改代码** → [开发文档](#开发文档)，外加根目录 [CONTRIBUTING.md](../CONTRIBUTING.md)
- **我要学 Redis 内核** → [学习文档](#学习文档)

## 开发文档

| 文档 | 内容 |
|---|---|
| [架构详解](dev/architecture.md) | seam 架构、backends.go 工厂接线、各后端组合的锁模型 |
| 如何添加一个命令（规划中） | handler → 注册 → 差分语料 → 四层测试 全流程实战 |
| 测试体系（规划中） | 单测/race、集成、差分、TCL 四层测试的定位与用法 |
| 配置参考（规划中） | 全部配置项分组说明 |

## 学习文档

以 hayakv 代码为教材的 Redis 内核教程（中文），从 RESP 协议到集群共 10 章——
见 [学习路线图](learn/README.md)。

## 运维与历史档案

| 文档 | 内容 |
|---|---|
| [ops-tuning.md](ops-tuning.md) | GOGC / GOMEMLIMIT 调优指南与内存分配基线 |
| [phase2-final-report.md](phase2-final-report.md) | Phase 2（Redis 8.x 对齐）验收报告 |
