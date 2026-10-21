# AI Workbench 前端

React 18 + Vite + TypeScript + Ant Design 5。页面与交互约定见 [docs/frontend-design.md](../docs/frontend-design.md)。

范围：任务工作台（我的任务、待认领、我经手的）、任务详情（卡片、业务字段、流程图、认领 / 开始处理 / 已处理 / 无需处理 / 推迟 / 升级 / 转交 / 优先级、历史）、任务类型（新建、四步配置与节点链画布、试跑 SQL、卡片预览、校验、发布、只读详情）。

## 本地运行

需要 MySQL 8.0 或 TiDB。建两个库：平台库 `ai_workbench`（执行 `deploy/sql/platform.sql`）和 mock 业务库 `biz_mock`（执行 `deploy/mock/appeal.sql`）。在仓库根目录：

```bash
# 1. 发布示例任务类型（配置里的 DSN 按本地 MySQL 修改）
go run ./cmd/wbctl publish -f app/worker/etc/worker.yaml examples/task_types/*.yaml

# 2. 后端：worker（执行 SQL 来源，首次 tick 即生成任务）、gRPC 服务、开发网关
go run ./app/worker -f app/worker/etc/worker.yaml
go run ./app/rpc -f app/rpc/etc/workbench.yaml
go run ./cmd/devgw

# 3. 前端（http://127.0.0.1:5173）
cd web && npm install && npm run dev
```

Vite 把 `/byai_platform` 与 `/dev` 代理到 devgw（默认 `http://127.0.0.1:8088`，可用环境变量 `WB_GATEWAY` 覆盖）。

## 身份

右上角「切换身份」读取 devgw 的 `GET /dev/users`（来自 `deploy/mock/vip_rm.yaml`、示例任务类型的候选人与兜底人、`permissions.yaml`），选中的身份经 `X-Dev-User` 请求头传给后端。演示路径：

- `rm.alice@example.com`：我的任务中有 A1001（待审核）、A1002（已等待 48 小时，可升级：随机交给两位组长之一）；升级或转交后在「我经手的」里只读可见；
- `lead@example.com`：我的任务中有 A1007（没有 RM，交给兜底人）；
- `reviewer.dave@example.com` / `reviewer.erin@example.com`：「待认领」里有 A1003（超过 7 天，发到 VIP 审核群），先认领的人处理；认领后可以转交给别人或放回待认领；
- `ops@example.com`：有 `task.admin`、`task_type.publish`，可以转交任意任务、发布任务类型。

## 目录

```text
src/api/client.ts     接口客户端：POST + JSON、bytes 字段的 base64 编解码、错误、身份请求头
src/api/types.ts      proto 消息的 JSON 形态（snake_case，int64 为字符串）与任务类型 JSON
src/api/services.ts   各 service 的调用封装
src/components/       TaskCard（后端渲染的卡片）、TaskBoardCard（工作台卡片）、TaskActions（任务操作按钮）、
                      ChainCanvas（节点链画布）、JsonEditor（CodeMirror）
src/components/tasktype/  任务类型的四步：SourceStep、FieldsStep、NodesStep、CardStep（编辑与只读共用）
src/pages/            MyTasks、TaskDetail、TaskTypes、TaskTypeDetail、TaskTypeEditor
```

`npm run typecheck` 做类型检查，`npm run build` 产出 `dist/`。
