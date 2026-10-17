# ai-workbench

企业内部 AI 任务平台：把各类数据源（cron 扫描、Kafka、API 推送）归一成事件，按配置生成任务、分配处理人、驱动 workflow，通过 Lark 触达。首批场景是 VIP 工单提醒和周期业务报表。

## 先读这两份设计

- `docs/ai-task-platform.md`：核心概念（事件类型、任务类型、workflow、展示模板、分配策略）、任务状态与优先级、功能模块。
- `docs/workflow-engine.md`：自研轻量工作流引擎（不使用 Temporal）的数据模型、执行模型、人工节点、xxl-job 时钟、受控的环、与任务中心的集成、试运行。

设计文档的原稿在 Claude Docs 中维护，这里是导出副本；改动设计时两边同步。

## 技术栈与约定

- Go 1.24，工具库为 [go-zero](https://github.com/zeromicro/go-zero)。
- **对外业务接口使用 gRPC**：协议写在 `app/rpc/workbench.proto`（RunService、TaskService、PlanService），由 `goctl rpc protoc` 生成 zrpc 服务。
  - 业务 JSON 字段用 `bytes`（UTF-8 JSON）承载，不用 `google.protobuf.Struct`，避免数字变 double 丢精度。
  - proto 的 `TaskStatus` 比存储值偏移 2（首值必须为 0），转换统一用 `internal/task` 的 `StatusToProto` / `StatusFromProto`。
- **HTTP 只用于第三方回调**：`app/callback/callback.api` 只有 Lark 卡片回调（Lark 以 HTTP 推送），由 `goctl api` 生成。
- 数据访问层由 `goctl model` 从 `deploy/sql/*.sql` 生成到 `internal/model`。
  - `*_gen.go` 每次生成都会覆盖，**不要手工修改**；自定义查询写在对应的 `*_model.go`。
  - 改表结构或接口时先改 `.sql` / `.proto` / `.api`，再 `make gen`。
- 存储为 TiDB（MySQL 协议），外部时钟为 xxl-job，平台只依赖这两者，不引入 Temporal、Redis 等额外服务。
- goctl 的 DDL 约束：单列主键（联合主键改为 `id` + 联合唯一键）、主键 `AUTO_INCREMENT`；TiDB 专用选项写在 `/*T! ... */` 注释中。

## 目录

```text
app/rpc/          go-zero zrpc 服务：对外 gRPC 接口（workbench.proto；生成后含 pb/server/logic/svc/etc）
app/callback/     go-zero HTTP 服务：仅 Lark 卡片回调（callback.api）
app/worker/       引擎执行进程：Worker 池、推进器、xxl-job ticker
deploy/sql/       表结构（goctl model 的输入）
internal/model/   goctl 生成的数据访问层
internal/task/    任务状态、优先级、转移表、任务中心接口
internal/assign/  分配策略链、认领、转派
internal/flow/    dsl（图模型）、compiler（编译与校验）、engine（推进器）、nodes（节点执行器）
internal/action/  InvokeAction：所有外部 IO 的统一出口
internal/view/    展示模板、受众变体、AI 摘要、渲染器
internal/lark/    Lark 发送、回调验签、卡片更新
internal/ticker/  xxl-job 调度入口（wf-ticker）
internal/event/ ingest/ subscription/ registry/ mcp/ pkg/
```

依赖方向单向：`internal/flow` 不得直接 import `task`、`assign`、`lark`，只能通过 `internal/action` 调用。

## 必须遵守的设计规则

- **任务状态取值对齐 Morning Brief**：`TaskStatusUnassigned=-1`、`Pending=0`、`Processing=1`、`Snoozed=2`、`Solved=3`，`>= Solved` 即终态。命名一律用 task，不用 signal。
- **优先级** `TaskPriorityP0..P3`（0 最紧急），由业务方定义，平台提供 `UpdatePriority`。
- **单人决策**：每个人工节点只由一个人决策；多人参与就拆成多个人工节点串联。
- **引擎是流程的事实来源，任务表是投影**：节点事件经 `sync_task` job 异步同步，任务中心只接受更大的 `seq`。
- **写入入口**：影响流程的操作（选择、推迟、退回）走引擎 Signal；开始处理、认领、调整优先级、转派直接走任务中心。
- **推进可复现**：外部 IO 在事务外执行；图的推进只在锁 `wf_run` 行的悲观事务内完成；遍历编译期排好序的切片，不遍历 map；不在推进逻辑里读系统时间（用注入的 Clock）。
- **至少执行一次**：所有外部动作带幂等键 `run_id:node_id:iteration[:seq]`。
- **金额不经过 float64**：schema 中金额为 string，解码用 `UseNumber()`，计算用 decimal。
- **出口统一脱敏**：按 schema 的 `x-pii` 在发往 Lark、LLM、日志时处理。
- **AI 用在设计时**：AI 生成的配置必须通过同一编译器校验，只能存草稿；发布需审批与试运行。

## 常用命令

```bash
make gen     # goctl 生成 rpc、callback api 与 model，然后 go mod tidy
make check   # go vet + go test + go build
```

## 当前进度

骨架阶段：领域类型与接口已定义，`.proto`、`.api` 与 `.sql` 已就绪，生成代码尚未提交。下一步是引擎阶段一（见 `docs/workflow-engine.md`「分阶段实现」）。
