# ai-workbench

企业内部 AI 任务平台。数据输入与处理流程解耦：任何来源的数据先归一成事件，再按配置生成任务、分配处理人、驱动 workflow，并通过 Lark 触达处理人。

- 设计：[AI 任务平台设计概要](docs/ai-task-platform.md) · [轻量工作流引擎设计](docs/workflow-engine.md)
- 技术栈：Go 1.24 · [go-zero](https://github.com/zeromicro/go-zero)（zrpc / gRPC）· TiDB / MySQL 8.0 · xxl-job · Lark

## 快速开始

```bash
go install github.com/zeromicro/go-zero/tools/goctl@latest
goctl env check --install --verbose --force   # 安装 protoc 与 protoc-gen-go / protoc-gen-go-grpc

make gen     # 由 workbench.proto 与 deploy/sql/*.sql 生成代码
make check   # vet + test + build
```

数据库初始化：执行 `deploy/sql/` 下的 `engine.sql`、`task.sql` 与 `config.sql`。

## 目录

| 路径 | 说明 |
| --- | --- |
| `app/rpc` | 对外 gRPC 接口（zrpc），经公司 gateway 转为 HTTP：配置、变更集、运行、任务、回调 |
| `app/worker` | 引擎执行进程：Worker 池、推进器、xxl-job ticker |
| `deploy/sql` | 表结构，goctl model 的输入 |
| `internal/` | 领域模块：任务、分配、流程引擎、动作、视图、配置中心、Lark 等 |
| `docs/` | 设计文档 |

开发约定见 [CLAUDE.md](CLAUDE.md)。
