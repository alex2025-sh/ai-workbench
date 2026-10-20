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
| 团队 | 角色只有分配者与执行者；认领模式发到团队 Lark 群，分配模式私发给分配者（如审核小组：组长分配，组员执行） |
| 个人关系查不到人 | 节点必填兜底团队，进入兜底团队的认领队列 |
| escalate 之后 | 经手人仍能查看整个流程，但不能处理（工作台「我经手的」） |
| 新版本对在途任务 | 在途任务按创建时的版本走完节点链；数据源扫描与字段刷新用最新版本 |
| 发布 | 直接发布，靠 `task_type.publish` 权限控制；审批平台接入放二期 |
| SQL | 前期由研发编写或配置；平台只开放只读从库连接，执行时只读事务、超时、行数限制 |
| Lark | 一期只发通知，不接卡片回调；卡片上只有 URL 按钮（在工作台处理、业务链接），所有动作在工作台完成 |

## 运行时

任务行是流程状态的唯一事实来源。所有变化都是「事件 → 状态转移」，事件驱动，不轮询业务。

状态取值对齐 Morning Brief：unassigned=-1（在团队队列）、pending=0、processing=1、snoozed=2、solved=3（终态）；任务行另记当前节点 `node_id`。

| 事件 | 来源 | 前置状态 | 结果 |
| --- | --- | --- | --- |
| 创建 / 进入节点 | 数据源、escalate | — | 个人 → pending（处理人 = 解析结果）；团队 → unassigned（队列 = 团队） |
| 认领 | 执行者（认领模式）或分配者（分配模式） | unassigned | pending，写入处理人 |
| 分配 | 分配者（分配模式） | unassigned | pending，处理人 = 选中的成员 |
| 开始处理 | 处理人 | pending | processing |
| snooze | 处理人 | 未完结 | snoozed，记录 `snooze_until` |
| 推迟到期 | ticker | snoozed | pending，重发 Lark |
| escalate | 处理人 | 未完结 | 进入下一个节点 |
| close / no_response | 处理人 | 未完结 | solved |
| key 消失 | 数据源 | 任意 | 未完结的 → solved(`auto_closed`)；解除跟踪 |
| 转派 / 调整优先级 | 处理人或 `task.admin` | 未完结 | 换处理人 / 改优先级，节点不变 |

任务还在队列里时，能认领的人可以直接处理，处理即认领。

转移是纯函数 `task.Apply(def, task, event) → (task', notify)`：不读时钟、不做 IO（成员角色、下一个节点的处理人由调用方事先求好），用表驱动测试覆盖。

一次事件的处理（`task.Store.Do`）：

1. 开事务，`select ... for update` 锁任务行（MySQL READ-COMMITTED）；
2. 求调用人在任务当前团队中的角色，调 `Apply`；非法转移直接拒绝（「任务已被处理」）；
3. 同一事务里写：任务行、`task_event`（历史）、`task_participant`（经手人）、`outbox`（待发的 Lark 通知），提交；
4. 提交后立即投递 outbox；失败由 ticker 退避重试（1 分钟起翻倍，最多 6 小时，10 次后放弃），幂等键 `task_id:version:kind:target`。

先提交、后触达；外部动作至少一次并带幂等键。

ticker：xxl-job 每分钟触发 `wb-ticker`，做三件事：到期的 SQL 数据源（`source_state` 用租约保证多实例下只有一个在跑）、到期的推迟、失败的通知重试。没有常驻工作池。

## 数据表

存储为 MySQL 8.0（兼容 TiDB），外部只依赖 xxl-job。表结构见 `deploy/sql/platform.sql`。

| 表 | 用途 |
| --- | --- |
| `task_type` / `task_type_version` | 任务类型与草稿 / 已发布版本（不可变） |
| `team` / `team_member` | 团队与成员角色 |
| `task` | 任务与流程状态；`track_key` + 唯一索引保证同一 key 只有一个跟踪中的任务 |
| `task_event` | 任务历史 |
| `task_participant` | 经手人（转走后仍能只读查看） |
| `outbox` | 待投递的 Lark 通知 |
| `source_state` | SQL 来源的调度与上一次结果 |

数据源连接与业务关系是服务配置，不入库。

## 接口

对外只有 gRPC（`app/rpc/workbench.proto`），前端经公司 gateway 访问，路径写在每个方法的行尾注释里。

| 服务 | 方法 |
| --- | --- |
| TaskTypeService | CreateTaskType、ListTaskTypes、GetTaskType（含数据源状态）、ListTaskTypeVersions、GetTaskTypeDraft、SaveTaskTypeDraft、DiscardTaskTypeDraft、ValidateTaskTypeDraft、PublishTaskTypeDraft、TrialSql、PreviewCard |
| TeamService | ListTeams、GetTeam、SaveTeam |
| TaskService | ListTasks（mine / queue / involved）、GetTask、ClaimTask、AssignTask、StartProcessing、ActTask、Reassign、UpdatePriority |
| CatalogService | ListDatasources、ListRelations |
| IngestService | Push |

接口级权限（BSP，本地见 `deploy/mock/permissions.yaml`）：`task.admin`（查看与转派任意任务、调整优先级）、`task_type.publish`、`team.admin`。任务的查看权限：当前处理人、经手人、当前团队成员、`task.admin`。

## 代码结构

| 包 | 职责 |
| --- | --- |
| `internal/tasktype` | 任务类型的 Go 模型、校验、字段占位模板、卡片渲染与脱敏 |
| `internal/task` | 状态、`Apply` 状态机、存储（事务、历史、经手人、outbox）、查询与权限 |
| `internal/source` | SQL 来源（只读查询、结果集对比）与推送 |
| `internal/assign` | 节点分配器求值、业务关系（本地 mock） |
| `internal/team` | 团队与成员 |
| `internal/notify` | outbox 投递、Lark（本地 mock，打到日志） |
| `internal/registry` | 草稿、校验（含引用检查）、发布、版本 |
| `internal/ticker` | xxl-job 调度入口 |
| `internal/bootstrap` | 组装（app/rpc、app/worker、wbctl 与端到端测试共用） |
