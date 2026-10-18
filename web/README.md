# AI Workbench 前端 demo

React 18 + Vite + TypeScript + Ant Design 5。页面与交互约定见 [docs/frontend-design.md](../docs/frontend-design.md)。

范围：我的任务、可认领、任务详情（卡片、流程操作、认领 / 开始处理 / 调整优先级 / 转派、进度、事件）、运行监控（取消运行、重试节点、跳过节点）、任务类型（新建、草稿编辑与 workflow 画布、校验、试运行、开发环境发布）。

## 本地运行

需要 MySQL 8.0 或 TiDB，建三个库并执行 `deploy/sql`：平台库 `ai_workbench`、试运行库 `ai_workbench_dryrun`，以及 mock 业务库 `biz_mock`（执行 `deploy/mock/appeal.sql`）。在仓库根目录：

```bash
# 1. 发布示例任务类型
WB_DSN='root:@tcp(127.0.0.1:3306)/ai_workbench?parseTime=true&loc=UTC&charset=utf8mb4' \
  go run ./cmd/wbctl publish examples/task_types/vip_appeal_alert.yaml

# 2. 后端：worker（cron 扫描，约 1 分钟后生成任务）、gRPC 服务、开发网关
go run ./app/worker -f app/worker/etc/worker.yaml
go run ./app/rpc -f app/rpc/etc/workbench.yaml
go run ./cmd/devgw

# 3. 前端（http://127.0.0.1:5173）
cd web && npm install && npm run dev
```

Vite 把 `/byai_platform` 与 `/dev` 代理到 devgw（默认 `http://127.0.0.1:8088`，可用环境变量 `WB_GATEWAY` 覆盖）。

## 身份

右上角「切换身份」读取 devgw 的 `GET /dev/users`（来自 `deploy/mock/vip_rm.yaml` 与 `groups.yaml`），选中的身份经 `X-Dev-User` 请求头传给后端。演示路径：

- `rm.alice@example.com`：我的任务中有 A1001、A1002；
- `lead@example.com`：可认领中有升级后的 A1003，可以认领、开始处理、调整优先级、转派，或进入详情点「已处理」完成任务；
- `ops@example.com`：有 `task.admin`、`run.admin`，可以转派任意任务，在运行详情中取消运行、重试或跳过节点（权限见 `deploy/mock/permissions.yaml`）；
- 任意身份：运行监控、任务类型页的试运行。

## 目录

```text
src/api/client.ts     接口客户端：POST + JSON、bytes 字段的 base64 编解码、错误、身份请求头
src/api/types.ts      proto 消息的 JSON 形态（snake_case，int64 为字符串）
src/api/services.ts   各 service 的调用封装
src/components/       TaskCard（后端渲染的卡片）、DryRunPanel（试运行）、JsonEditor（CodeMirror）
src/components/canvas/       workflow 画布：model（端口、默认配置、自动排版）、WorkflowCanvas、FlowNode、NodeConfigPanel
src/components/schemaform/   按 JSON Schema 生成节点配置表单；labels.ts 为字段的中文标签
src/pages/            MyTasks、TaskDetail、Runs、RunDetail、TaskTypes、TaskTypeDetail、TaskTypeEditor
```

`npm run typecheck` 做类型检查，`npm run build` 产出 `dist/`。
