# ai-workbench

企业内部 AI 任务平台：把业务数据（定时 SQL、API、Kafka）归一成待办，按配置生成任务、分配处理人，在任务工作台处理，通过 Lark 通知。首批场景是 VIP 工单提醒。

> 本分支 `simplify` 是简化版设计，不合并到 main：任务类型 = 数据源 + 业务字段 + 人工节点链 + 卡片，用任务行上的状态机替代工作流引擎。

## 先读这几份设计

- `docs/ai-task-platform.md`：决策、运行时（状态转移、事件处理、outbox、ticker）、数据表、接口、代码结构。
- `docs/task-type-spec.md`：任务类型 JSON（前后端契约）的字段说明。
- `docs/frontend-design.md`：前端页面与交互（任务工作台、任务详情、四步配置）。

设计原稿在 Claude Docs（「任务平台简化方案」）中，这里是随代码维护的版本；改动设计时两边同步。

## 技术栈与约定

- Go 1.24，工具库为 [go-zero](https://github.com/zeromicro/go-zero)。
- **对外接口只有 gRPC**：协议写在 `app/rpc/workbench.proto`（TaskTypeService、TaskService、CatalogService、IngestService），由 `goctl rpc protoc -m` 生成 zrpc 服务。
  - 前端经公司 gateway 访问，路径约定 `/byai_platform/api/v1/<资源>/<动作>`，一律 POST，写在每个方法的行尾注释里。
  - 调用者身份由 gateway 写入 gRPC metadata（key 见 `app/rpc/internal/identity`），请求消息里不放 actor 字段。
  - 业务 JSON 字段用 `bytes`（UTF-8 JSON）承载，不用 `google.protobuf.Struct`，避免数字变 double 丢精度。
  - proto 的 `TaskStatus` 比存储值偏移 2（首值必须为 0），转换统一用 `internal/task` 的 `StatusToProto` / `StatusFromProto`。
  - 领域错误到 gRPC 状态码的映射、TaskSummary / TaskDetail 的组装集中在 `app/rpc/internal/rpcx`。
- **没有 HTTP 服务**，一期也不接 Lark 卡片回调（只发通知）。本地开发用的 `cmd/devgw` 模拟 gateway 的 HTTP 转换与身份注入，不部署。
- 数据访问层由 `goctl model`（固定 v1.10.2）从 `deploy/sql/*.sql` 生成到 `internal/model`，生成结果已提交，CI 校验与 SQL 一致。
  - `*_gen.go` 每次生成都会覆盖，**不要手工修改**；自定义查询写在对应的 `*_model.go`（`WithSession` 已导出，供事务内使用）。
  - 改表结构或接口时先改 `.sql` / `.proto`，再 `make gen`。
- 存储为 MySQL 8.0（兼容 TiDB，只用两者共有的能力），外部时钟为 xxl-job，平台只依赖这两者。
- goctl 的 DDL 约束：单列主键、主键 `AUTO_INCREMENT`；TiDB 专用选项写在 `/*T! ... */` 注释中。

## 目录

```text
api/schema/        task_type.schema.json：任务类型 JSON 的结构契约，嵌入到 Go 中校验
app/rpc/           go-zero zrpc 服务（workbench.proto；internal/rpcx 是 logic 共用的转换）
app/worker/        时钟进程：ticker（本地定时器或 xxl-job），执行 SQL 来源、唤醒推迟、重试通知
cmd/wbctl/         命令行：validate / json / publish 任务类型
cmd/devgw/         仅本地开发：HTTP POST + JSON → gRPC，X-Dev-User 写入 metadata；GET /dev/users 列出 mock 身份
web/               前端（React + Vite + TypeScript + Ant Design），运行方式见 web/README.md
examples/          任务类型示例（VIP 工单按时间窗口拆成三个），测试保证它们始终能通过校验
deploy/sql/        表结构（goctl model 的输入）
deploy/mock/       本地 mock：业务方的 appeal 表、VIP → RM 关系、接口级权限（不进 goctl）
internal/model/    goctl 生成的数据访问层
internal/tasktype/ 任务类型的 Go 模型、校验、字段占位模板、卡片渲染与脱敏
internal/task/     任务状态与优先级、Apply 状态机、存储（事务、历史、经手人、outbox）、查询与权限
internal/source/   SQL 来源（只读查询、结果集对比）与推送
internal/assign/   分配器（relation + 兜底人 / pool 随机 / claim 群认领）、业务关系、选择策略
internal/notify/   outbox 投递与 Lark（本地 mock）
internal/registry/ 草稿、校验（含数据源 / 关系引用）、发布、版本
internal/ticker/   xxl-job 调度入口（wb-ticker）
internal/bootstrap/ 组装（app/rpc、app/worker、wbctl 与端到端测试共用；testenv.go 是测试环境）
internal/directory/ 接口级权限（BSP；本地读 deploy/mock/permissions.yaml）
internal/pkg/      cursor（分页游标）、yamljson（YAML 1.2 转 JSON）、testdb（集成测试临时库）、obs
```

## 必须遵守的设计规则

- **任务状态取值对齐 Morning Brief**：`TaskStatusUnassigned=-1`、`Pending=0`、`Processing=1`、`Snoozed=2`、`Solved=3`，`>= Solved` 即终态。命名一律用 task。
- **优先级** `TaskPriorityP0..P3`（0 最紧急），新任务取任务类型的 `priority`。
- **流程只有线性的人工节点链**：start → 节点 → end；`escalate` 进入下一个节点，`close` / `no_response` 结束。不加条件、定时、并行、环等节点，时间类规则写成数据源 SQL 的时间窗口。
- **分配只有三种**：`relation`（业务关系查人，查不到交给兜底人）、`pool`（一批人中随机）、`claim`（通知到 Lark 群，任何人认领，先到先得；可配置无人认领时再提醒一次群）。新方式实现 `assign.Resolver` 并注册，不加团队与成员管理。
- **单人决策**：每个节点只由一个人决策；待认领的任务先认领再处理。升级换节点，转交（`transfer`，必须写原因）在同一节点内换人或放回待认领。
- **节点 id 不让业务填**：前端自动生成且不展示；任务行记录节点 id，所以发布后不改 id。
- **任务行是流程状态的唯一事实来源**：状态转移只经 `task.Apply`（纯函数：不读时钟、不做 IO，所需的角色与下一个节点的处理人由调用方事先求好），在 `task.Store.Do` 锁任务行的短事务中写回；连接使用 READ-COMMITTED。
- **先提交、后触达**：通知与状态转移在同一事务写入 outbox，提交后投递，失败由 ticker 退避重试；所有外部动作带幂等键。
- **事件驱动，不轮询业务**：变化只来自数据源（定时 SQL、推送）、工作台操作和 ticker（到期的数据源、推迟、通知重试）。不引入常驻工作池。
- **数据源的语义**：key「在」→ 没有跟踪中的任务就创建、有则刷新字段；「不在」→ 自动关闭。手工完结的任务保持跟踪，key 消失一次后再出现才是新任务。SQL 来源的结果被截断时不关闭。
- **金额不经过 float64**：金额字段为 `money`（字符串），解码用 `UseNumber()`。
- **出口统一脱敏**：按字段的 `pii` 在工作台、Lark、日志中处理（`tasktype.Display`）。
- **任务类型 JSON 只有一份契约**：改字段时同时改 `api/schema/task_type.schema.json`、`internal/tasktype` 的 Go 类型、`web/src/api/types.ts` 和 `docs/task-type-spec.md`；示例必须继续通过 `go test ./...`。模板只有字段占位 `{字段名}`，没有表达式语言。
- **版本**：任务类型是发布单元，发布后版本不可变；在途任务按创建时的版本走完节点链，数据源用最新版本。一期直接发布（`task_type.publish` 权限），审批平台放二期。
- **权限**：任务的查看权限是当前处理人、经手人与 `task.admin`，待认领的任务所有人可见；只有当前处理人能处理，`task.admin` 可以转交任意任务。
- **外部系统**：人员与组织、接口级权限来自 BSP；业务关系（如 VIP → RM）来自关系服务；SQL 来源只连平台注册的只读从库连接。

## 常用命令

```bash
make gen     # 生成 rpc 与 model，然后 go mod tidy
make check   # go vet + go test + go build
make validate  # 用 wbctl 校验 examples/ 下的任务类型
go run ./cmd/wbctl publish -f app/worker/etc/worker.yaml examples/task_types/*.yaml  # 本地发布示例
go run ./app/worker -f app/worker/etc/worker.yaml  # 时钟进程（本地每 30 秒一次 tick）
go run ./app/rpc -f app/rpc/etc/workbench.yaml     # gRPC 服务（127.0.0.1:9090）
go run ./cmd/devgw     # 本地 HTTP 网关（127.0.0.1:8088 → 9090）
cd web && npm run dev  # 前端（127.0.0.1:5173，代理到 devgw）
```

集成测试需要 MySQL 8.0 或 TiDB，设置 `WB_TEST_MYSQL=root:@tcp(127.0.0.1:3306)/` 后运行 `go test ./...`；未设置时这些测试跳过。CI 中用 MySQL 服务容器运行。

外部系统目前都是本地实现：Lark 为 `notify.Mock`（卡片打到日志），业务关系为 `assign.StaticRelations`（读 `deploy/mock/vip_rm.yaml`），接口级权限为 `directory.StaticPermissions`（读 `deploy/mock/permissions.yaml`）。

## 当前进度

- 已完成：任务类型契约与校验；三种分配器；状态机与任务中心（认领、开始处理、已处理、无需处理、推迟、升级、转交、待认领再次提醒、优先级）；SQL 来源与推送；outbox 与 Lark 通知（私聊与发群，mock）；ticker；gRPC 接口；wbctl、devgw；前端（任务工作台三个 Tab、任务详情、四步配置与节点链画布、任务类型只读详情）。VIP 三个任务类型（relation + 兜底人、pool 随机、claim 群认领）端到端测试通过。
- 未完成：Lark 真实接入（卡片回调与卡片更新放二期）、Kafka 消费、关系服务与 BSP 的真实接入、审批平台。
