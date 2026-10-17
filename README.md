# ai-workbench

企业内部 AI 任务平台。数据输入与处理流程解耦：任何来源的数据先归一成事件，再按配置生成任务、分配处理人、驱动 workflow，并通过 Lark 触达处理人。

- 设计：[AI 任务平台设计概要](docs/ai-task-platform.md) · [轻量工作流引擎设计](docs/workflow-engine.md) · [任务类型 JSON 说明](docs/task-type-spec.md)
- 技术栈：Go 1.24 · [go-zero](https://github.com/zeromicro/go-zero)（zrpc / gRPC）· TiDB / MySQL 8.0 · xxl-job · Lark

## 快速开始

```bash
make check      # vet + test + build（集成测试需要 WB_TEST_MYSQL，见 CLAUDE.md）
make validate   # 校验 examples/ 下的任务类型
```

在本地 MySQL 8.0 上跑 VIP 工单提醒：

```bash
mysql -e "CREATE DATABASE ai_workbench; CREATE DATABASE biz_mock"
cat deploy/sql/*.sql | mysql ai_workbench           # 平台表
mysql biz_mock < deploy/mock/appeal.sql             # 业务方 appeal 表的 mock 与样例数据

export WB_DSN='root:@tcp(127.0.0.1:3306)/ai_workbench?parseTime=true&loc=UTC&charset=utf8mb4'
go run ./cmd/wbctl publish examples/task_types/vip_appeal_alert.yaml
go run ./app/worker -f app/worker/etc/worker.yaml   # 每分钟扫描一次，卡片打印在日志中

# 模拟组长在升级卡片上点击「已处理」，并查看 run
go run ./cmd/wbctl signal -run <run_id> -node escalate -option handled -actor lead@example.com
go run ./cmd/wbctl run <run_id>
```

代码生成：`make gen`（需要 protoc、protoc-gen-go、protoc-gen-go-grpc 与 goctl v1.10.2）。

## 目录

| 路径 | 说明 |
| --- | --- |
| `api/schema` | 任务类型 JSON 的结构契约，前端可直接使用 |
| `cmd/wbctl` | 校验与导出任务类型的命令行工具 |
| `examples` | 任务类型示例 |
| `app/rpc` | 对外 gRPC 接口（zrpc），经公司 gateway 转为 HTTP：配置、变更集、运行、任务、回调 |
| `app/worker` | 引擎执行进程：Worker 池、推进器、xxl-job ticker |
| `deploy/sql` | 表结构，goctl model 的输入 |
| `deploy/mock` | 本地 mock：业务方的 appeal 表与样例数据、VIP → RM 关系 |
| `internal/` | 领域模块：任务、分配、流程引擎、动作、视图、配置中心、Lark 等 |
| `docs/` | 设计文档 |

开发约定见 [CLAUDE.md](CLAUDE.md)。
