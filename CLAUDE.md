# ai-workbench

企业内部 AI 任务平台：把各类数据源（cron 扫描、Kafka、API 推送）归一成事件，按配置生成任务、分配处理人、驱动 workflow，通过 Lark 触达。首批场景是 VIP 工单提醒和周期业务报表。

## 先读这几份设计

- `docs/ai-task-platform.md`：核心概念（事件类型、任务类型、workflow、展示模板、分配策略）、任务状态与优先级、功能模块。
- `docs/task-type-spec.md`：任务类型 JSON（前后端契约）的字段说明。
- `docs/workflow-engine.md`：自研轻量工作流引擎（不使用 Temporal）的数据模型、执行模型、人工节点、xxl-job 时钟、受控的环、与任务中心的集成、试运行。

设计文档的原稿在 Claude Docs 中维护，这里是导出副本；改动设计时两边同步。

## 技术栈与约定

- Go 1.24，工具库为 [go-zero](https://github.com/zeromicro/go-zero)。
- **对外接口只有 gRPC**：协议写在 `app/rpc/workbench.proto`（TaskTypeService、ComponentService、ChangeSetService、CatalogService、IngestService、RunService、TaskService、CallbackService），由 `goctl rpc protoc -m` 生成 zrpc 服务。
  - 前端经公司 gateway 访问，路径约定 `/byai_platform/api/v1/<资源>/<动作>`，一律 POST。一期不引入 gateway 注解，路径写在每个方法的行尾注释里；接入 gateway 时再改为 `option (bgw.v1.method_options)`。
  - 调用者身份由 gateway 写入 gRPC metadata，请求消息里不放 actor / viewer 字段。
  - 业务 JSON 字段用 `bytes`（UTF-8 JSON）承载，不用 `google.protobuf.Struct`，避免数字变 double 丢精度。
  - proto 的 `TaskStatus` 比存储值偏移 2（首值必须为 0），转换统一用 `internal/task` 的 `StatusToProto` / `StatusFromProto`。
- **没有 HTTP 服务**：Lark 卡片回调与审批平台回调由 gateway 原样透传 raw body 与 headers，落到 gRPC 方法（`CallbackService.LarkCardCallback`、`ChangeSetService.OnApprovalResult`），在服务内验签。
- 数据访问层由 `goctl model`（固定 v1.10.2）从 `deploy/sql/*.sql` 生成到 `internal/model`，生成结果已提交，CI 校验与 SQL 一致。
  - `*_gen.go` 每次生成都会覆盖，**不要手工修改**；自定义查询写在对应的 `*_model.go`（`WithSession` 已导出，供事务内使用）。
  - 改表结构或接口时先改 `.sql` / `.proto`，再 `make gen`。
- 存储为 TiDB 或 MySQL 8.0（MySQL 协议，只用两者共有的能力；自增 id 不保证单调，排序用 `seq`），外部时钟为 xxl-job，平台只依赖这两者，不引入 Temporal、Redis 等额外服务。
- goctl 的 DDL 约束：单列主键（联合主键改为 `id` + 联合唯一键）、主键 `AUTO_INCREMENT`；TiDB 专用选项写在 `/*T! ... */` 注释中。

## 目录

```text
api/schema/       task_type.schema.json：任务类型 JSON 的结构契约（前端、YAML、AI 生成共用），嵌入到 Go 中校验
cmd/wbctl/        命令行：校验 YAML / JSON、导出 body_json 与执行计划、节点类型目录
examples/         任务类型示例（vip_appeal_alert.yaml），测试保证它们始终能通过校验
app/rpc/          go-zero zrpc 服务：对外 gRPC 接口（workbench.proto；生成后含 pb/server/logic/svc/etc）
app/worker/       引擎进程（main + etc/worker.yaml）：ticker（本地定时器或 xxl-job），触发 cron 来源与到期 job；无常驻工作池
deploy/sql/       表结构（goctl model 的输入）
deploy/mock/      本地 mock：业务方的 appeal 表与样例数据、VIP → RM 关系（不进 goctl）
internal/model/   goctl 生成的数据访问层
internal/task/    任务状态、优先级、转移表、任务中心接口
internal/assign/  分配策略链、认领、转派
internal/spec/    任务类型的 Go 模型与完整校验（结构 → 语义 → 编译 workflow）
internal/flow/    dsl（图模型、节点配置、端口目录）、compiler（编译与校验）、engine（推进器）、nodes（节点执行器）
internal/action/  InvokeAction：所有外部 IO 的统一出口
internal/view/    展示模板、受众变体、AI 摘要、渲染器
internal/lark/    Lark 发送、回调验签、卡片更新
internal/ticker/  xxl-job 调度入口（wf-ticker）
internal/registry/ 配置中心：草稿、变更集、审批、发布清单（表结构见 deploy/sql/config.sql）；direct.go 是开发用的直接发布
internal/bootstrap/ 组装引擎、任务中心、动作实现与 ticker（app/worker、wbctl 与端到端测试共用）
internal/ingest/  cron 来源：扫描 skill → 过滤 → 映射 → 建任务 → 启动 run
internal/skill/   平台内置 skill（appeal：VIP 场景的工单扫描与状态查询）
internal/approval/ directory/ skillhub/  外部系统客户端：公司审批平台、BSP、Skillhub
internal/pkg/     expr（表达式与模板，基于 expr-lang）、schema（x- 扩展与脱敏）、yamljson（YAML 1.2 转 JSON）、testdb（集成测试临时库）、obs
internal/event/ subscription/ mcp/
```

依赖方向单向：`internal/flow` 不得直接 import `task`、`assign`、`lark`，只能通过 `internal/action` 调用。

## 必须遵守的设计规则

- **任务状态取值对齐 Morning Brief**：`TaskStatusUnassigned=-1`、`Pending=0`、`Processing=1`、`Snoozed=2`、`Solved=3`，`>= Solved` 即终态。命名一律用 task，不用 signal。
- **优先级** `TaskPriorityP0..P3`（0 最紧急），由业务方定义，平台提供 `UpdatePriority`。
- **单人决策**：每个人工节点只由一个人决策；多人参与就拆成多个人工节点串联。
- **引擎是流程的事实来源，任务表是投影**：节点事件经任务同步 effect（`sync_task`）异步同步，任务中心只接受更大的 `seq`。
- **写入入口**：影响流程的操作（选择、推迟、退回）走引擎 Signal；开始处理、认领、调整优先级、转派直接走任务中心。
- **处理人归属**：引擎只在流程明确分配时写处理人（节点确定处理人、有人做了决定，`sync_task` 带 `reassign`）；其余同步保留任务中心的处理人，认领、转派不会被覆盖。
- **先提交、后触达**：人工节点先提交 `waiting`，再经 `effect` job 发卡片；会让外部看到流程状态的动作都在状态提交之后执行。
- **事件驱动，不轮询**：流程只由事件推进（来源启动 run、人工 Signal、外部状态变化 Cancel、xxl-job tick 唤醒到期 job），事件处理完后就地执行新就绪的节点；`wf_job` 只存将来或待恢复的工作。不引入常驻工作池。
- **推进可复现**：外部 IO 在事务外执行；流程状态与 job 状态（含认领）只经引擎的 `update` 在锁 `wf_run` 行的悲观事务内修改，状态转换只改内存、由 `flush` 统一写回；遍历编译期排好序的切片，不遍历 map；不在推进逻辑里读系统时间（用注入的 Clock）。`wf_job` 只按主键更新，连接使用 READ-COMMITTED。
- **至少执行一次**：所有外部动作带幂等键 `run_id:node_id:iteration[:seq]`。
- **金额不经过 float64**：schema 中金额为 string，解码用 `UseNumber()`，计算用 decimal。
- **出口统一脱敏**：按 schema 的 `x-pii` 在发往 Lark、LLM、日志时处理。
- **任务类型 JSON 只有一份契约**：改字段时同时改 `api/schema/task_type.schema.json`、`internal/flow/dsl` / `internal/spec` 的 Go 类型和 `docs/task-type-spec.md`；示例必须继续通过 `go test ./...`。表达式不能读取当前时间（禁用 `now()`）。
- **AI 用在设计时**：AI 生成的配置必须通过同一编译器校验，只能存草稿；发布需审批与试运行。
- **版本模型**：任务类型是发布单元，schema、来源、workflow、专属模板内嵌其中；共享组件（view / assignment / agreement / prompt）独立版本化，以 `key@version` 固定引用，没有“跟随最新”。运行时只按发布清单（`cfg_manifest_item`）取配置。
- **审批在外部**：使用公司审批平台；变更集状态机在平台内，提交时快照草稿，审批通过后重新编译再原子发布，回调必须幂等。
- **外部系统**：人员与组织、接口级权限来自 BSP（前端经 gateway 直连 BSP 选人）；skill 来自 Skillhub 后端 API；Kafka 来源由生产方遵循平台的 topic 与消息格式，平台不支持动态 topic。
- **namespace**：配置与变更集带 `namespace` 字段；按 namespace 的数据级权限暂不实现。

## 常用命令

```bash
make gen     # 生成 rpc 与 model，然后 go mod tidy
make check   # go vet + go test + go build
make validate  # 用 wbctl 校验 examples/ 下的任务类型
```

集成测试（引擎、端到端）需要 MySQL 8.0 或 TiDB，设置 `WB_TEST_MYSQL=root:@tcp(127.0.0.1:3306)/` 后运行 `go test ./...`；未设置时这些测试跳过。CI 中用 MySQL 服务容器运行。

外部系统目前都是本地实现：Lark 为 `internal/lark` 的 Mock（卡片打到日志），分配服务为 `assign.Local`（关系表读 `deploy/mock/vip_rm.yaml`），邮件、事件、LLM 为 `action.Logger`。

## 当前进度

- 已完成：任务类型 JSON 契约与编译器；引擎阶段一与阶段二的主体（事件驱动的推进器、重试与防护、timer、human_choice 的等待 / 信号 / 超时、cron 来源与 `close_when`、任务状态同步、xxl-job ticker）；VIP 工单提醒在本地端到端跑通。
- 未完成：Lark 真实接入与卡片回调、重复提醒与 snooze、受控的环（rework）运行期、共享组件（`key@version`）解析、gRPC 服务实现、变更集与审批。
