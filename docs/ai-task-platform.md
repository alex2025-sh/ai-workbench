# AI 任务平台设计（简化版）

> 本分支（`simplify`）的设计。原稿为 Claude Docs 中的「任务平台简化方案」，这里是随代码维护的版本。

企业内部的任务平台：把业务数据（定时 SQL、API、Kafka）归一成「待办」，按配置生成任务、分配处理人，在任务工作台处理，通过 Lark 通知。首批场景是 VIP 工单提醒。

**任务类型 = 数据源 + 业务字段 + 人工节点链 + 卡片**（契约见 [task-type-spec.md](task-type-spec.md)）。流程只有一种形状：start → 人工节点 A → B → C → end，`escalate` 交给下一个节点，`close` / `no_response` 结束。执行方式是任务行上的状态机，没有工作流引擎。

不做（以后真有需求再按需加，不预留抽象）：条件分支、定时节点、skill / LLM 节点、并行与汇合、退回环、旁路动作、共享组件库、变更集与审批、AI 生成配置、Lark 卡片回调。

## 已确认的决策

| 决策 | 结论 |
| --- | --- |
| 流程模型 | 线性的人工节点链，画布保留但只是节点链的展示（连线由动作生成） |
| 执行方式 | 任务行上的状态机；没有 run、引擎到任务表的同步、seq |
| 时间类规则（48h、7d） | 属于业务数据：cron SQL 按时间窗口扫出来，作为独立的任务类型；节点上不配超时 |
| cron 来源语义 | SQL 结果集就是当前待办：新出现的 key 建任务，消失的 key 自动关闭；不做「一轮关闭过多时暂停」的阈值 |
| close / no_response | 都结束任务，resolution 分别为 `handled` / `no_response`，只在统计上区分 |
| 卡片 | 一个任务类型一张卡片，自动显示当前节点名；需要差异时拆任务类型 |
| 分配 | 三种分配器：业务关系（如 VIP → RM）+ 兜底人；一批人中随机；群里认领（不限人员，先到先得）。一期不做团队与成员管理 |
| 认领 | 认领节点通知到 Lark 群，打开链接的人自己认领；待认领在工作台对所有人可见；节点可配置「N 小时无人认领再提醒一次群」，不自动升级 |
| 转交 | 节点动作 `transfer`（默认启用）：同一节点内交给另一个人，认领节点还可以放回待认领；必须写原因，历史记 from → to，详情显示转交次数；`task.admin` 始终可以转交 |
| 升级、转交、认领的区别 | 升级换节点；转交在同一节点内换人；认领是从群里领走 |
| 节点 id | 前端自动生成且不展示，业务不需要配置 |
| escalate 之后 | 经手人仍能查看整个流程，但不能处理（工作台「我经手的」） |
| 新版本对在途任务 | 在途任务按创建时的版本走完节点链；数据源扫描与字段刷新用最新版本 |
| 发布 | 直接发布，靠 `task_type.publish` 权限控制；审批平台接入放二期 |
| SQL | 前期由研发编写或配置；平台只开放只读从库连接，执行时只读事务、超时、行数限制 |
| Lark | 一期只发通知，不接卡片回调；卡片上只有 URL 按钮（在工作台处理、业务链接），所有动作在工作台完成 |

## 运行时

任务行是流程状态的唯一事实来源。所有变化都是「事件 → 状态转移」，事件驱动，不轮询业务。

状态取值对齐 Morning Brief：unassigned=-1（待认领，只出现在认领节点）、pending=0、processing=1、snoozed=2、solved=3（终态）；任务行另记当前节点 `node_id`。

| 事件 | 来源 | 前置状态 | 结果 |
| --- | --- | --- | --- |
| 创建 / 进入节点 | 数据源、escalate | — | relation / pool → pending（处理人 = 求值结果，私聊通知）；claim → unassigned（通知到群，记录 `remind_at`） |
| 认领 | 任何人 | unassigned | pending，处理人 = 认领人；先到先得，后到的人得到「已被认领」 |
| 再次提醒 | ticker | unassigned 且到了 `remind_at` | 再发一次群通知，清空 `remind_at`（只提醒一次） |
| 开始处理 | 处理人 | pending | processing |
| snooze | 处理人 | 未完结 | snoozed，记录 `snooze_until` |
| 推迟到期 | ticker | snoozed | pending，重发 Lark |
| escalate | 处理人 | 未完结 | 进入下一个节点 |
| close / no_response | 处理人 | 未完结 | solved |
| key 消失 | 数据源 | 任意 | 未完结的 → solved(`auto_closed`)；解除跟踪 |
| 转交 | 处理人（节点启用 `transfer`）或 `task.admin`，必须写原因 | 未完结 | 交给某人 → pending 并私聊新处理人；放回待认领（只限认领节点）→ unassigned 并重新通知群；节点不变 |
| 调整优先级 | 处理人或 `task.admin` | 未完结 | 改优先级 |

待认领的任务先认领再处理；只有当前处理人能处理。

转移是纯函数 `task.Apply(def, task, event) → (task', notify)`：不读时钟、不做 IO（下一个节点的处理人由调用方经 `assign.Service` 事先求好），用表驱动测试覆盖。

一次事件的处理（`task.Store.Do`）：

1. 开事务，`select ... for update` 锁任务行（MySQL READ-COMMITTED）；
2. 调 `Apply`；非法转移直接拒绝（「任务已被处理」「已被认领」）；
3. 同一事务里写：任务行、`task_event`（历史）、`task_participant`（经手人）、`outbox`（待发的 Lark 通知），提交；
4. 提交后立即投递 outbox；失败由 ticker 退避重试（1 分钟起翻倍，最多 6 小时，10 次后放弃），幂等键 `task_id:version:kind:target`。

先提交、后触达；外部动作至少一次并带幂等键。

ticker：xxl-job 每分钟触发 `wb-ticker`，做三件事：到期的 SQL 数据源（`source_state` 用租约保证多实例下只有一个在跑）、到期的推迟与待认领的再次提醒、失败的通知重试。没有常驻工作池。

## 数据表

存储为 MySQL 8.0（兼容 TiDB），外部只依赖 xxl-job。表结构见 `deploy/sql/platform.sql`。

| 表 | 用途 |
| --- | --- |
| `task_type` / `task_type_version` | 任务类型与草稿 / 已发布版本（不可变） |
| `task` | 任务与流程状态；`track_key` + 唯一索引保证同一 key 只有一个跟踪中的任务；`remind_at` 是待认领的再次提醒时间 |
| `task_event` | 任务历史 |
| `task_participant` | 经手人（转走后仍能只读查看） |
| `outbox` | 待投递的 Lark 通知（`lark_user` 私聊、`lark_chat` 发群） |
| `source_state` | SQL 来源的调度与上一次结果 |

数据源连接与业务关系是服务配置，不入库。

## 接口

对外只有 gRPC（`app/rpc/workbench.proto`），前端经公司 gateway 访问，路径写在每个方法的行尾注释里。

| 服务 | 方法 |
| --- | --- |
| TaskTypeService | CreateTaskType、ListTaskTypes、GetTaskType（含数据源状态）、ListTaskTypeVersions、GetTaskTypeDraft、SaveTaskTypeDraft、DiscardTaskTypeDraft、ValidateTaskTypeDraft、PublishTaskTypeDraft、TrialSql、PreviewCard |
| TaskService | ListTasks（mine / claimable / involved）、GetTask、ClaimTask、StartProcessing、ActTask、TransferTask、UpdatePriority |
| CatalogService | ListDatasources、ListRelations |
| IngestService | Push |

接口级权限（BSP，本地见 `deploy/mock/permissions.yaml`）：`task.admin`（查看与转交任意任务、调整优先级）、`task_type.publish`。任务的查看权限：当前处理人、经手人、`task.admin`；待认领的任务所有人可见。

## 代码结构

| 包 | 职责 |
| --- | --- |
| `internal/tasktype` | 任务类型的 Go 模型、校验、字段占位模板、卡片渲染与脱敏 |
| `internal/task` | 状态、`Apply` 状态机、存储（事务、历史、经手人、outbox）、查询与权限 |
| `internal/source` | SQL 来源（只读查询、结果集对比）与推送 |
| `internal/assign` | 分配器（`Resolver`：relation / pool / claim）、业务关系（`Relations`，本地 mock）、选择策略（`Picker`：random） |
| `internal/notify` | outbox 投递、Lark（本地 mock，打到日志） |
| `internal/registry` | 草稿、校验（含引用检查）、发布、版本 |
| `internal/ticker` | xxl-job 调度入口 |
| `internal/bootstrap` | 组装（app/rpc、app/worker、wbctl 与端到端测试共用） |
