# AI 任务平台设计概要

Oct 15, 2026 · @rex

## 背景与目标

以 VIP 工单提醒和周期业务报表为首批场景，v1 直接按通用平台设计：**数据输入与处理流程解耦**，任何来源的数据先归一成事件，再按配置生成任务、分配处理人、驱动 workflow、通过 Lark 触达。

- **VIP 工单提醒**：交易命中规则产生工单 → 通知负责 RM；工单在 maker 状态超过 48h、7 天时再次提醒。
- **周期报表**：业务方通过 skill 生成报表，可设置为周期执行。
- **长期目标**：业务人员（RM、CS、运营）通过对话让 AI 生成定时任务和 workflow，人工审批后上线。

现有基建：Openclaw（对话入口）、LiteLLM（模型网关）、大数据 / 业务 DB / TiDB MCP、业务接口 MCP、Skillhub。

## 核心概念

平台由三类东西组成：**输入侧**（事件从哪来）、**任务侧**（要处理什么、谁处理、怎么展示）、**流程侧**（按什么步骤处理）。所有配置都版本化，发布后不可修改，只能发布新版本。

| 概念 | 定义 | 关键内容 |
| --- | --- | --- |
| 事件类型 Event Type | 一类输入数据的命名与结构，与来源无关 | 名称（如 `ticket.created`、`ticket.maker_overdue`）、payload schema、owner、数据敏感等级 |
| 触发器 Trigger | 产生事件的方式 | 类型：cron + skill 扫描 / Kafka topic / API 推送；调度表达式与时区；skill 及参数；字段映射；幂等键 |
| 订阅 Subscription | 把某类事件转成某类任务的规则 | 事件类型 + 过滤条件 → 任务类型；payload 映射；去重键（如 `ticket_id:48h`） |
| 任务类型 Task Type | 任务的抽象定义，平台的核心配置单元 | 名称 + 版本；payload JSON Schema（含 `x-pii`、`x-format` 等扩展）；默认优先级；SLA 策略；绑定的 workflow、展示模板、分配策略 |
| 任务实例 Task | 某个任务类型的一次具体任务 | 公共列（status、priority、assignee、sla\_status、resolution、escalation\_level、due\_at、dedup\_key、type\_version）+ JSON payload |
| 任务事件 Task Event | 任务的每一次变更记录 | 谁、何时、从哪个状态到哪个状态、原因；同时是审计数据和统计数据来源 |
| Workflow 定义 | 处理任务的流程，图模型 DSL | 节点 + 端口 + 连线；发布时编译成执行计划 Plan；运行实例锁定版本 |
| 节点类型 Node Type | Workflow 中可用的步骤种类 | 原生节点（condition、merge、timer、human\_choice）/ 动作节点（skill\_call、llm、lark\_card、send\_email、emit\_event）；配置 schema；输出端口 |
| Skill / 动作 Action | Activity 节点背后的可执行能力 | 分三类：查询（只读）、推理（LLM）、动作（有副作用）；输入输出 schema 契约 |
| 展示模板 View Template | 任务在各渠道的展示方式，与渠道无关 | header、fields、actions（按钮绑定状态机动作）、可选的 AI 摘要；渲染到 Lark 卡片 / Web 工作台 / 邮件 |
| 分配策略 Assignment Policy | 决定任务交给谁 | 候选人解析器 → 过滤器 → 选择策略 → 兜底；支持用户组、认领、管理员分配、轮询、按关系（RM-VIP） |
| 共享组件 Component | 被多个任务类型复用、独立版本的配置 | 共享卡片模板、分配策略、跨部门协议、提示词模板；以「组件@版本」固定引用；带 namespace |
| 变更集 Change Set | 审批与发布的单位 | 一个或多个草稿；在公司审批平台审批；通过后原子发布 |
| 发布清单 Release Manifest | 任务类型某个版本的完整依赖闭包，相当于锁文件 | 自有组件的内容哈希 + 共享组件的固定版本；任务实例与 run 只认清单 |

### 任务状态

主状态取值对齐 Morning Brief 的状态：原有 4 个取值和含义不变，只在最前面新增一个“待分配”状态，共 5 个；“>= 3 即终态”的判断保持成立。

```mermaid
stateDiagram-v2
    [*] --> unassigned: 处理人待认领
    [*] --> pending: 分配服务直接命中处理人
    unassigned --> pending: 分配 / 认领
    pending --> processing: 开始处理
    processing --> pending: 退回 / 转派
    pending --> snoozed: 推迟
    processing --> snoozed: 推迟
    snoozed --> pending: 到期回迁
    pending --> solved: 卡片上直接处理
    processing --> solved: 完成
    unassigned --> solved: 源工单离开 maker（auto_closed）
    pending --> solved: 源工单离开 maker（auto_closed）
    solved --> [*]
```

> 取值：unassigned = -1，pending = 0，processing = 1，snoozed = 2，solved = 3（`>= 3` 即终态）。

| 值 | 常量 | 含义 |
| --- | --- | --- |
| -1 | `TaskStatusUnassigned`（新增） | 待分配：在组队列中等待认领，或等待分配服务确定处理人 |
| 0 | `TaskStatusPending` | 已分配并投递给处理人，等待处理 |
| 1 | `TaskStatusProcessing` | 已接手（点过「开始处理」或外链） |
| 2 | `TaskStatusSnoozed` | 主动推迟，到期由 cron 回迁 pending |
| 3 | `TaskStatusSolved` | 已完结；`>= 3` 即终态 |

分配服务直接命中处理人（如按 RM-VIP 关系）时，任务创建即为 `pending`，跳过 `unassigned`。RM 可以在 Lark 卡片上一步处理，从 `pending` 直接进入 `solved`。转派只改变 assignee，状态不变。源工单离开待处理状态时（来源的 `close_when`），系统把未完结的任务置为 `solved`（auto_closed）。任务被认领、转派后，处理人以任务中心为准，引擎只在流程再次明确分配时更新它。

新增状态取 `-1` 而不是 `4`：已有的 0 到 3 不用重新编号，Morning Brief 的存量数据和 `>= TaskStatusSolved` 的终态判断都不受影响。需要确认 Morning Brief 侧没有“status 非负”之类的假设。`waiting` 归入 `processing`；`cancelled` 合并进 `solved`，由结果码区分。

正交字段：`resolution`（contacted / dismissed / escalated\_out / auto\_closed）、`snooze_until`（`snoozed` 状态的回迁时间）、`sla_status`（on\_track / at\_risk / breached）、`escalation_level`（0 / 1 / 2）。所有状态变更走同一个服务方法：校验转移表、乐观锁、写 Task Event。

### 任务优先级

取值与 Morning Brief 的优先级保持一致，数值越小越紧急：

| 值 | 常量 | 含义 |
| --- | --- | --- |
| 0 | `TaskPriorityP0` | 立即 |
| 1 | `TaskPriorityP1` | 今日 |
| 2 | `TaskPriorityP2` | 机会 |
| 3 | `TaskPriorityP3` | 维护 |

- **由业务方定义**：任务类型配置默认优先级，也可以在订阅中按规则计算（如按交易金额或客户等级的表达式），平台只负责求值和存储。
- **平台提供调整接口**：`UpdatePriority(task_id, priority, reason)`，按权限控制谁可以调整，原因必填，写入 Task Event；调整后同步更新已发出的 Lark 卡片和任务列表排序。
- 优先级是独立字段，与状态互不影响；任务列表与 Morning Brief 先按优先级、再按截止时间排序。

## 概念之间的绑定关系

任务类型是平台的核心配置单元：事件经订阅转成任务类型，任务类型再绑定 workflow、展示模板和分配策略，四者一起发布、一起版本化。

```mermaid
flowchart LR
    Trigger[触发器<br/>cron / Kafka / API] --> EventType[事件类型<br/>命名 + schema]
    EventType --> Sub[订阅<br/>过滤 + 映射 + 去重]
    Sub --> TaskType[[任务类型<br/>payload schema · 版本]]
    TaskType -- 绑定 --> WF[Workflow 定义<br/>图模型 DSL]
    TaskType -- 绑定 --> View[展示模板<br/>Lark / Web / 邮件]
    TaskType -- 绑定 --> Assign[分配策略<br/>解析 → 过滤 → 选择]
    TaskType -- 实例化 --> Task[任务实例<br/>status · assignee]
    Assign --> Task
    WF -- 由节点组成 --> NodeType[节点类型<br/>原生 / 动作]
    NodeType --> Skill[Skill / 动作<br/>查询 · 推理 · 动作]
    Task -- 每次变更写入 --> TaskEvent[任务事件<br/>状态变更 · 审计]
```

分配策略决定任务实例的 assignee；workflow 由节点组成，动作节点最终调用 skill 或外部系统；任务实例的每次变更都写入任务事件。

一个任务类型的绑定配置示例：

```yaml
task_type: vip_ticket_alert
version: 6                             # 发布后生成发布清单
namespace: cs_vip
schema: { ... }                        # 自有：payload JSON Schema
sources: [ ... ]                       # 自有：cron / Kafka / API 来源
workflow: { nodes: [ ... ], edges: [ ... ] }   # 自有：在画布中编辑
views:
  rm_card: { ... }                     # 自有
  compliance_card: compliance_card@1   # 共享组件，固定版本
view: rm_card                          # 默认展示模板
assignment:                            # 自有；也可引用共享策略，如 vip_rm_policy@4
  resolvers:
    - { type: relation, relation: vip_primary_rm }
    - { type: relation, relation: vip_backup_rm }
  filters: [active, not_on_leave, not_maker, within_capacity]
  strategy: least_load
  fallback: { group: vip_cs_leads, strategy: claim }
sla:
  - { after: 48h, action: remind }
  - { after: 7d,  action: escalate }
```

订阅侧示例：事件 `ticket.maker_overdue`（由每天 9:00 的 cron + `scan_maker_tickets` skill 产生）经过滤条件 `segment == 'vip'` 后，映射为 `vip_ticket_alert` 任务，去重键 `ticket_id:48h`。

### 节点级的展示模板与处理人分配

任务类型上的 `view` 和 `assignment` 只是默认值；workflow 中每个人工节点都可以覆盖，同一个任务在不同节点、推送给不同部门时，看到的卡片和处理人都可以不同。

```yaml
- id: rm_followup
  type: human_choice
  options: [contacted, escalate]
  # 不写 view / assignment：它是第一个人工节点，回退到任务类型默认（RM 跟进卡，按 RM-VIP 关系分配）

- id: compliance_review
  type: human_choice
  options: [approve, return]
  view:
    default: compliance_review_card@1
    variants:
      - { when: "viewer.dept == 'aml'", view: compliance_review_card_aml@1 }
  assignment:
    resolvers:
      - { type: agreement, agreement: cs_to_compliance@1 }   # 跨部门协议
    strategy: claim
    exclude: [task.maker, nodes.rm_followup.assignee]      # 职责分离
```

**展示模板的解析顺序**：节点上的受众变体 → 节点模板 → 上一个人工节点的模板 → 任务类型默认模板。

- **按节点**：不同环节展示不同字段，如 RM 跟进卡突出客户与联系方式，合规复核卡突出交易与规则命中明细。
- **按受众**：`variants` 按查看者的部门、角色选择模板；同一字段可按受众采用不同脱敏级别，跨部门人员只看到其环节所需的字段。
- **按钮来自节点**：卡片按钮由节点的 `options` 生成，模板只负责样式；编译期校验模板中的按钮与节点端口一一对应。

**人工节点的处理人分配**：

- **复用分配服务的策略链**（解析器 → 过滤器 → 选择 → 兜底），每个节点可以有自己的策略。
- **跨部门协议**：新增 `agreement` 解析器，引用配置中心中的跨部门协作协议（受理组、响应 SLA、升级联系人）。协议由对方部门维护和审批，我们的 workflow 只引用，不修改。
- **单人决策**：每个人工节点只由一个人做出决定；指派给用户组时，组内第一个认领的人成为处理人。需要多人参与时，拆成多个人工节点串联。
- **职责分离**：`exclude` 排除工单 maker、上游节点的处理人等，由平台强制执行。
- **当前处理人**：进入人工节点时，任务的 assignee 更新为该节点的处理人，并记录当前节点（通知节点确定接收人时同样更新）；Morning Brief 和 Lark 推送都发给当前处理人，任务表仍是唯一事实来源。

**未配置时的继承规则**：人工节点不写 `view` 或 `assignment` 时，沿用上一个人工节点的配置；路径上没有更早的人工节点时，回退到任务类型默认值。

- **编译期解析**：继承关系在发布时沿图计算并写入 Plan，运行时不再推断，每个节点最终用哪套模板和策略在发布前就能看到。
- **多个上游**：节点经 `merge` 汇合、不同路径上的“上一个人工节点”配置不一致时，编译报错，要求显式配置，避免运行时取决于走了哪条分支。
- **继承的是策略，不是人**：沿用上一节点的分配策略时会重新求值，结果通常是同一个人；需要固定为上一节点的实际处理人时，显式写 `assignment: { resolvers: [{ type: previous_assignee }] }`。
- **退回的环**：环内节点的继承同样在编译期确定，每一轮使用相同的模板和策略。
- **通知节点**：`lark_card` 不写 `view` 或 `to` 时按同样规则取值，但它不会成为下游节点的继承来源。

**AI 摘要（可选）**：配置 view 时可以开启 AI 对当前任务的总结摘要，摘要失败时卡片上不显示该区块。

```yaml
view:
  default: vip_ticket_card@2
  ai_summary:
    enabled: true
    prompt: vip_ticket_summary@1     # 提示词模板，版本化管理
    inputs: [rule_code, amount, maker_since, recent_txns]
    max_chars: 200
    timeout: 5s
```

- **先发后补，不阻塞触达**：卡片先发出，摘要通过 `llm` 动作异步生成，成功后更新原卡片。摘要永远不影响任务的推送和流转。
- **失败即隐藏**：输出为自然语言，格式与语气在提示词模板中约定；超时、模型报错、输出为空或超出 max\_chars 时，摘要区块整体不显示，卡片上不出现报错文案；失败只记入 Task Event 与指标。
- **按受众取数**：只把当前查看者可见的字段送给模型，并先按 `x-pii` 脱敏，避免跨部门推送时通过摘要泄露字段。
- **缓存**：按任务、节点与 payload 版本缓存，数据没有变化时不重复生成。
- **明确标识**：摘要区块标注“AI 生成，仅供参考”；workflow 的判断逻辑从不读取摘要内容。

## 功能模块

系统拆成 6 层 12 个模块。控制面（配置、AI 设计）只产出版本化配置；执行面（接入、任务、流程、触达）按配置运行，两者通过配置中心解耦。

| 层 | 模块 | 职责 | 关键实现 |
| --- | --- | --- | --- |
| 接入 | 1. 接入适配器 Ingestion | 把各种来源归一成标准事件信封 | Cron 调度执行 skill 扫描；Kafka 消费者；API / Webhook 网关；统一信封（type、subject、time、payload、dedup\_key） |
| 接入 | 2. 事件中心 Event Hub | 事件类型注册、校验、去重、分发 | 按事件类型 schema 校验；幂等键去重；写入内部 topic |
| 任务 | 3. 订阅路由 Subscription Router | 事件 → 任务创建请求 | 匹配订阅；过滤条件求值（expr）；payload 映射 |
| 任务 | 4. 任务中心 Task Service | 任务的唯一事实来源 | 状态机与转移表；乐观锁；Task Event 审计；SLA 计算；源工单状态变化时自动关闭任务 |
| 任务 | 5. 分配服务 Assignment Service | 决定处理人 | 策略链（解析器 → 过滤器 → 选择 → 兜底）；RM-VIP 关系；负载统计；认领 CAS；转派留痕；职责分离校验 |
| 流程 | 6. 流程引擎 Workflow Engine | 按 DSL 驱动任务前进 | 发布期编译器（校验、拓扑分析、生成 Plan）；轻量引擎的推进器（死路径消除、信号、受控的环、任务状态同步），详见《轻量工作流引擎设计》；节点执行器注册表 |
| 流程 | 7. 动作运行时 Action Runtime | 执行所有外部 IO | 引擎推进时调用的通用 `InvokeAction` 动作，分发到：skill 调用（MCP / Skillhub）、LLM（Eino → LiteLLM）、Lark、邮件、emit\_event |
| 触达 | 8. 视图渲染器 View Renderer | 模板 + 任务 → 各渠道内容 | 渠道无关视图 DSL；Lark 卡片 / Web / 邮件渲染器；按 `x-pii` 脱敏、按 `x-format` 格式化 |
| 触达 | 9. Lark 网关 Lark Gateway | 发送、回调、卡片更新 | 回调验签；open\_id → 内部用户；按任务表鉴权；调用 RunService.Signal；`task_id → message_id` 映射，状态变化时更新原卡片；降噪合并 |
| 控制面 | 10. 配置中心 Config Registry | 所有配置的版本与发布 | 任务类型（含自有组件）与共享组件；草稿 → 变更集 → 公司审批平台 → 原子发布；发布清单；dry-run 试运行 |
| 控制面 | 11. AI 设计助手 + 平台 MCP | 对话生成配置 | Openclaw / Eino agent 调用平台 MCP（list\_skills、get\_skill\_schema、list\_node\_types、validate、dry\_run、save\_draft）；校验 — 修复循环 |
| 横切 | 12. 公共库与可观测 | 各模块共用的能力 | JSON Schema 校验与 `x-` 扩展处理；expr 表达式；执行状态表 → 画布着色；指标（通知到联系时长、跟进率）；审计日志 |

前端包括三部分：配置控制台、workflow 画布（React Flow，设计态与运行态共用）、任务工作台（Uni-Workbench 内的任务列表与详情）。

## 内部架构与调用链路

控制面只产出并发布配置；执行面沿一条主链路把事件变成 RM 手上的 Lark 卡片，RM 的操作再沿虚线回到流程引擎。

```mermaid
flowchart TB
    subgraph CP[控制面]
        AI[AI 设计助手<br/>Openclaw · Eino agent] --> MCP[平台 MCP<br/>validate · dry_run · 草稿]
        MCP --> Reg[[配置中心<br/>版本 · 审批 · 发布]]
        FE[前端<br/>控制台 · 画布 · 工作台] --> Reg
    end
    Reg -- 发布版本化配置 --> RT
    subgraph RT[执行面]
        Ing[接入适配器<br/>cron · Kafka · API] --> Hub[事件中心<br/>校验 · 去重 · 分发]
        Hub --> Router[订阅路由<br/>过滤 · 映射]
        Router --> TaskSvc[任务中心<br/>状态机 · SLA · 审计]
        TaskSvc -- 启动 workflow --> Engine[流程引擎<br/>编译器 · 推进器]
        Engine --> Action[动作运行时<br/>skill · LLM · 邮件]
        Action --> AssignSvc[分配服务<br/>策略链 · 认领]
        Action --> Render[视图渲染器<br/>按模板 · 脱敏]
        Render --> Lark[Lark 网关<br/>发送 · 回调 · 更新卡片]
        Lark -. RM 点击按钮 → 按任务表鉴权 → RunService.Signal .-> Engine
    end
    RT --- Infra[(TiDB · Kafka · xxl-job · LiteLLM · Skillhub / MCP · Lark 开放平台)]
```

主链路：接入适配器产生标准事件 → 事件中心校验去重 → 订阅路由生成任务 → 任务中心落库并启动 workflow → 流程引擎通过动作运行时调用分配服务、skill、LLM → 视图渲染器生成卡片 → Lark 网关发送。公共库与可观测模块横跨所有模块，图中未单独画出。

### Go 工程内部分包

仓库采用 go-zero 工程约定：对外接口全部是 gRPC，由 `goctl rpc` 从 `app/rpc/workbench.proto` 生成 zrpc 服务；数据访问层由 `goctl model` 从 `deploy/sql/*.sql` 生成；领域逻辑放在 `internal/` 下。

```text
app/
  rpc/                 # 对外 gRPC 接口（经公司 gateway 暴露给前端与外部系统，含 Lark 回调）
    workbench.proto
  worker/              # 引擎进程：cron 来源与 xxl-job ticker（事件驱动，无常驻工作池）
deploy/sql/            # 表结构，goctl model 的输入
internal/
  model/               # goctl model 生成的数据访问层
  ingest/              # 接入适配器、事件信封
  event/               # 事件类型注册、校验、去重
  subscription/        # 订阅匹配与映射
  task/                # 任务状态、优先级、转移表、Task Event、SLA
  assign/              # 分配策略链、认领、转派
  flow/
    dsl/               # 图模型定义
    compiler/          # 校验、拓扑分析、编译期继承、生成 Plan
    engine/            # 推进器、认领与租约、边传播、区域重置、状态同步
    nodes/             # 节点执行器注册表
  action/              # InvokeAction 分发：skill、llm、lark、email、emit_event
  view/                # 视图模板、受众变体、AI 摘要、渲染器
  lark/                # 发送、回调验签、卡片更新
  ticker/              # xxl-job 调度入口
  registry/            # 草稿、变更集、发布清单、dry-run
  approval/            # 公司审批平台适配
  directory/           # BSP 人员与组织适配
  skillhub/            # Skillhub 后端 API 适配
  mcp/                 # 平台 MCP 工具
  pkg/
    schema/            # JSON Schema 校验、x- 扩展（脱敏、格式化）
    expr/              # 表达式编译与求值
    obs/               # 指标、审计、链路
```

依赖方向保持单向：`flow` 不直接依赖 `task`、`assign`、`lark`，只通过 `action` 的动作执行器调用它们，这样引擎核心不会因业务模块变化而改动。

## 前后端交互

前端与所有外部调用方统一经公司 gateway 访问平台的 gRPC 接口：gateway 负责 HTTP 与 gRPC 的协议转换、认证和限流，平台只暴露 gRPC。

### 接入方式

- **协议**：接口全部写在 proto 中，每个方法用 gateway 的 `bgw.v1.method_options` 声明 HTTP 路径、方法和限流规则。路径约定为 `/byai_platform/api/v1/<资源>/<动作>`，统一使用 POST。
- **身份**：调用人身份由 gateway 认证后写入 gRPC metadata，服务端从 metadata 读取；请求消息中不出现 `actor`、`viewer` 这类由前端填写的身份字段。
- **权限**：接口级权限在 BSP 权限平台按方法配置，由 gateway 执行，平台内不实现权限判断。
- **Lark 回调同样走 gateway**：gateway 透传原始请求体与请求头，回调作为 gRPC 方法接收，平台不再单独部署 HTTP 服务。

```protobuf
rpc SaveTaskTypeDraft(SaveTaskTypeDraftReq) returns (SaveTaskTypeDraftResp) {
  option (bgw.v1.method_options) = {
    path       : "/byai_platform/api/v1/task_type/draft/save";
    http_method: HTTP_METHOD_POST;
    filters    : [{ name: FILTER_BIZ_LIMITER_V2; args: '--rules=[{"rate":20, "ip":true, "path":true}]' }];
  };
}
```

### 页面与操作

| 页面 | 用户动作 | 对应服务 |
| --- | --- | --- |
| 任务类型列表 | 按 namespace 筛选、新建、查看版本历史 | TaskTypeService |
| 任务类型编辑 | 编辑字段 schema、默认优先级与 SLA、来源、workflow、自有卡片模板，校验与试运行 | TaskTypeService |
| Workflow 画布 | 拖入节点、配置节点、连线、实时校验 | TaskTypeService（workflow 是任务类型草稿的一部分）、CatalogService |
| 节点配置面板 | 选择节点类型、按配置 schema 生成的表单填写、选择 skill 与共享组件 | CatalogService、ComponentService |
| 卡片模板编辑 | 编辑字段与样式、受众变体、AI 摘要，用样例数据预览 | TaskTypeService（自有）或 ComponentService（共享） |
| 组件库 | 管理共享卡片模板、分配策略、跨部门协议、提示词模板，查看引用方 | ComponentService |
| 变更与发布 | 把草稿加入变更集、一键升级依赖方、提交审批、查看版本差异 | ChangeSetService |
| 运行监控 | 按任务查 run、运行态画布、事件时间线、人工干预 | RunService |
| 我的任务 | 列表、详情、认领、开始处理、调整优先级、转派 | TaskService |

人员、用户组选择器由前端直接调用 BSP（已经通过 gateway 开放），平台只保存用户与组的 ID。

### 任务类型 JSON

前端提交给 `SaveTaskTypeDraft` 的 `body_json` 是一份完整的任务类型 JSON，结构契约是仓库中的 [`api/schema/task_type.schema.json`](../api/schema/task_type.schema.json)（JSON Schema draft 2020-12）。仓库里手写的 YAML、AI 生成的配置都是同一种 JSON，经过同一个编译器校验。字段说明见 [任务类型 JSON 说明](task-type-spec.md)。

- **顶层结构**：`key`、`name`、`schema`（payload JSON Schema）、`sources`、`dedup_key`、`views`、`view` / `assignment`（默认值）、`workflow`（`nodes` + `edges`）、`ui`。
- **节点配置表单**：`CatalogService.ListNodeTypes` 返回的 `config_schema_json` 从同一份 schema 中抽出，前端据此生成表单；端口规则（固定端口、按分支、按选项）也一并返回。
- **表达式**：条件、映射、锚点时间等字段直接写表达式（如 `payload.vip_level > 0`），其余字符串是模板，用 `{{ }}` 插值。可用的命名空间是 `payload`、`task`、`nodes.<id>.output`、`loop`；来源映射中只能用 `record`，受众变体中额外可用 `viewer`。表达式不能读取当前时间。
- **校验结果**：问题统一为 `code`、`severity`（error / warning）、`path`、`message`。`path` 用 ID 定位，如 `workflow.nodes.ask_rm.config.timeout`、`workflow.edges.e3`，前端可直接高亮画布上的节点和连线。
- **`ui` 字段**：画布坐标等纯展示数据放在这里，后端原样保存，编译器忽略。

## 配置生命周期

任务类型与共享组件共用同一套生命周期：草稿可反复修改，发布后不可变；审批与发布的单位是变更集。

```mermaid
stateDiagram-v2
    [*] --> 草稿
    草稿 --> 变更集: 加入（打包、编译校验）
    变更集 --> 审批中: 提交（公司审批平台）
    审批中 --> 已发布: 通过（原子发布、生成清单）
    审批中 --> 草稿: 驳回 / 撤回（草稿解锁，继续编辑）
    已发布 --> 已下线: 下线（不再创建新任务）
```

- **草稿**：每个任务类型、每个共享组件同时最多一份草稿。「编辑」已发布的配置时，系统自动基于最新发布版本创建草稿。保存带 `revision`，两人同时编辑时后保存者收到冲突错误，不会互相覆盖。
- **变更集**：把一个或多个草稿打包。加入时对整个依赖闭包编译校验，存在问题则不允许提交。共享组件升级时，可以一键把依赖方的升级草稿加入同一个变更集。
- **审批中**：变更集提交后在公司审批平台创建审批实例，所含草稿锁定不可编辑。驳回或撤回后草稿解锁。
- **已发布**：审批通过后，变更集中的所有版本在一个事务内发布，生成发布清单；不会出现任务类型已发布、它引用的共享模板还是草稿的情况。
- **已下线**：下线的任务类型不再创建新任务，在途任务继续跑完；仍被引用的共享组件版本不能下线。

## 版本管理

**任务类型是发布单元**：只服务于它的 workflow 与卡片模板随它一起发版，用户只看到一个版本号；被多个任务类型复用的组件独立发版，引用时固定版本；发布时生成不可变的发布清单，运行时只认清单。

版本之间是一张依赖图，而不是平铺的「任务类型 | workflow 版本 | 模板版本」：卡片模板挂在 workflow 的各个人工节点上，一个 workflow 可能引用多个模板，模板也可能被多个任务类型共享。

### 自有组件与共享组件

| | 自有组件 | 共享组件 |
| --- | --- | --- |
| 包括 | 任务类型的 payload schema、来源与订阅、workflow、只服务于它的卡片模板 | 共享卡片模板、分配策略、跨部门协议、提示词模板 |
| 编辑方式 | 在任务类型的同一个草稿里编辑 | 在组件库中单独编辑 |
| 版本 | 没有独立版本号，只记内容哈希，随任务类型版本发布 | 独立版本号 |
| 引用方式 | 直接内嵌（相当于 Camunda 的 `deployment` 绑定） | `组件@版本` 固定引用（相当于 `versionTag` 绑定） |

自有组件可以通过「另存为共享组件」提升到组件库，当前任务类型的草稿随之改为引用 `@1`。

### 发布清单

任务类型发布时，系统解析整个依赖闭包，写成一份不可变的清单（相当于锁文件）：

```text
vip_ticket_alert v6
  schema        sha256:ab12…
  workflow      （自有）sha256:cd34…   → wf_plan(vip_ticket_alert, 6)
  views         rm_card（自有）、compliance_card@1（共享）
  assignment    vip_rm_policy@4（共享）
  agreements    cs_to_compliance@2（共享）
```

- 任务实例绑定 `type_version`，run 绑定对应的清单；在途实例不受新版本影响。
- 编译后的 workflow 计划按 `(任务类型, 任务类型版本)` 存入 `wf_plan`，不再单独对用户暴露 workflow 版本号。
- 不提供「跟随最新版本」的绑定，避免改一个模板就悄悄影响线上所有流程。

### workflow 变了，任务类型要不要升版本

- **自有 workflow**：它就是任务类型的一部分，改 workflow 就是改任务类型草稿，发布后直接是任务类型的下一个版本。
- **共享组件变了**：引用方需要升版本。系统列出所有引用旧版本的任务类型，一键为它们生成升级草稿，与组件新版本放进同一个变更集，一次审批、一起发布。

### 取舍

| 目标 | 做法 |
| --- | --- |
| 正确性 | 发布后不可变；实例固定版本；在途实例不受影响；共享依赖固定版本；发布前对整个依赖闭包编译校验 |
| 便捷性 | 用户只面对一个版本号；自有组件不单独管版本；共享组件升级时自动生成依赖方的升级草稿；编辑时自动基于最新发布版本建草稿 |

这一模型参考 Camunda 的[资源绑定类型](https://docs.camunda.io/docs/components/best-practices/modeling/choosing-the-resource-binding-type/)：自包含的资源用 `deployment` 绑定，共享依赖用版本标签固定，`latest` 只适合开发与紧急修复。

## 任务来源配置

三类来源都在任务类型页面的「来源」中配置，属于任务类型的自有组件；系统自动生成对应的事件类型与订阅，用户不直接管理这两层。

| 来源 | 模式 | 在平台内配置的内容 | 前端的试运行 |
| --- | --- | --- | --- |
| cron | 平台主动扫描 | 查询 skill 与参数、cron 表达式与时区、过滤条件、关闭条件、字段映射、去重键 | 立即试跑一次扫描，展示会产出多少条与样例 |
| Kafka | 控制反转：生产方按平台定义的 topic 与标准事件信封生产 | 事件类型名、过滤条件、字段映射、去重键 | 贴一条样例消息，展示映射结果 |
| API | 外部系统调用 `IngestService.PushEvent`，请求体同样是标准事件信封 | 事件类型名、过滤条件、字段映射、去重键 | 生成调用示例，贴样例请求展示映射结果 |

- Kafka 采用控制反转：topic 与消息格式由平台定义，生产方按约定生产，配置变更不涉及新增或修改 topic。确有需要接入外部 topic 时再单独开发。
- Kafka 与 API 两类来源由生产方在信封中标明事件类型，平台按事件类型路由到对应的任务类型。
- cron 来源发布后落到 `wf_schedule`，由 xxl-job ticker 触发。
- 关闭条件（`close_when`）让外部状态变化直接推动流程：记录满足条件时，按去重键找到未完结的任务，取消它的 run，任务以 `auto_closed` 完结。例如工单离开待审核，提醒立即停止，不必等到下一个定时检查点。
- 去重键要能区分同一件事的不同轮次：VIP 工单用“工单号 + 提交时间”，驳回后重新提交会重新提醒。
- cron 扫描遇到暂时性失败（如数据库抖动）时不推进游标，下次重扫；单条坏数据跳过并计数。
- 多个来源可以对应同一个任务类型，共用去重键。

## 外部系统集成边界

平台只实现任务、流程与配置版本本身；人员组织、权限、审批、skill 这些通用能力都使用公司已有的系统。

| 系统 | 用途 | 调用方式 | 平台保存什么 |
| --- | --- | --- | --- |
| BSP | 人员、用户组、组织查询 | 前端选择器直接调用（已通过 gateway 开放）；后端在分配处理人、映射 Lark open\_id 时经 `internal/directory` 适配层查询，带短期缓存 | 只保存用户 ID 与组 ID |
| BSP 权限平台 | 接口级权限 | 在 BSP 按 gRPC 方法配置，由 gateway 执行 | 无 |
| 公司审批平台 | 配置发布审批 | 提交变更集时创建审批实例；审批结束后经 gateway 回调 `ChangeSetService.OnApprovalResult` | 变更集状态、审批实例 ID |
| Skillhub | 查询、推理、动作类 skill | 后端 API；前端的 skill 列表与 schema 由 `CatalogService` 转发，节点执行由动作运行时调用 | 节点配置中的 skill 名与参数 |
| Lark | 卡片触达与交互 | 发送与更新卡片由 Lark 网关调用；卡片回调经 gateway 透传原始请求体与请求头，由 `CallbackService.LarkCardCallback` 验签 | `task_id → message_id` 映射 |

### 审批的职责划分

| 公司审批平台 | 平台 |
| --- | --- |
| 审批流转、审批人规则、待办与通知 | 配置版本状态机与发布动作 |
| 起草人不能审批自己的变更 | 提交时创建审批实例并记录实例 ID |
| 审批单中放平台的版本差异链接 | 提供变更集的差异对比页面与接口 |
| 审批结束后回调 | 校验回调，通过则原子发布，驳回则退回草稿 |

审批结果只是触发发布的信号；发布前平台重新编译变更集中的全部草稿，确认审批期间没有被改动。

## 接口清单

所有接口定义在 `app/rpc/workbench.proto`，由 `goctl rpc protoc` 生成 zrpc 服务。业务 JSON 字段一律用 `bytes` 承载，避免 `google.protobuf.Struct` 把金额变成 double。

| 服务 | 职责 | 方法 |
| --- | --- | --- |
| TaskTypeService | 任务类型及其自有组件（schema、来源、workflow、自有模板） | CreateTaskType、ListTaskTypes、GetTaskType、GetTaskTypeDraft、SaveTaskTypeDraft、DiscardTaskTypeDraft、ValidateTaskTypeDraft、DryRunTaskTypeDraft、GenerateSamplePayload、PreviewView、TrialSource、ListTaskTypeVersions |
| ComponentService | 共享组件：卡片模板、分配策略、跨部门协议、提示词模板 | CreateComponent、ListComponents、GetComponent、GetComponentDraft、SaveComponentDraft、DiscardComponentDraft、ValidateComponentDraft、ListComponentVersions、ListDependents |
| ChangeSetService | 变更集、审批与发布 | CreateChangeSet、AddDependentUpgrades、GetChangeSet、ListChangeSets、DiffChangeSet、SubmitChangeSet、WithdrawChangeSet、OnApprovalResult、RetireVersion |
| CatalogService | 画布与配置表单的数据源 | ListNodeTypes（含配置 schema、UI 提示、端口规则）、ListSkills、GetSkillSchema（转发 Skillhub） |
| IngestService | API 来源 | PushEvent |
| RunService | 工作流运行 | StartRun、Signal、CancelRun、GetRun、ListRuns、GetRunEvents、RetryNode、SkipNode |
| TaskService | 任务处理 | GetTask、ListMyTasks、ListClaimableTasks、GetTaskEvents、ClaimTask、StartProcessing、UpdatePriority、Reassign |
| CallbackService | 第三方回调 | LarkCardCallback |

### 暂不考虑

- **数据级权限**：所有配置带 `namespace` 字段，v1 只用于分组与筛选，不做权限校验；后续再扩展为按 namespace 的数据级权限。
- **动态 Kafka topic**：生产方按平台定义的 topic 生产，需要接入外部 topic 时再开发。
- **「跟随最新版本」的绑定**：只提供固定版本引用。
- **在途实例迁移**：新版本只影响新任务，在途 run 按原清单跑完。

## 关键设计原则

1. **AI 用在设计时，执行保持确定性。** 是否超时、通知谁由 SQL 和规则决定；AI 生成的是配置，必须通过与人工配置相同的校验器，只能保存为草稿。
2. **配置版本化、不可变。** 任务实例绑定 `type_version`，workflow 实例锁定 DSL 版本；修改即发布新版本。
3. **Schema 驱动。** 新增任务类型只需注册 schema、模板和策略，Go 代码不改动；平台层透传 `json.RawMessage`，少数类型用泛型 `Decode[T]` 获得强类型。
4. **金额不经过 float64。** schema 中金额用 string，解码开启 `UseNumber()`，业务侧用 decimal。
5. **引擎核心保持薄而稳定。** 变化放进动作与节点类型：新增动作只在 `InvokeAction` 注册实现，推进器代码不变。
6. **推进过程可复现。** 遍历编译期排好序的切片而非 map；外部 IO 在事务外执行，图的推进只在锁 run 行的事务内完成。
7. **任务表是处理人的唯一事实来源。** API 层按任务表鉴权，引擎的 Signal 只校验节点是否等待、选项是否合法。
8. **DSL 严格受限。** 只支持有限的节点类型，只允许受控的环（rework 边），不支持脚本；复杂关键流程直接用 Go 写成系统节点。
9. **控制状态大小。** 大输出存对象存储，状态表只保存引用；事件日志按月分区归档。
10. **自动化本身也要 maker-checker。** 配置发布需审批；dry-run 是必经步骤；AI 能用的 skill 与数据范围不超过当前用户权限。
11. **出口统一脱敏。** 按 `x-pii` 在发往 Lark、LLM、日志时处理；工单 maker 不能被分配为 checker。
12. **回执闭环。** 每次触达都要能收到 RM 的反馈，回写任务并产生指标。

## v1 范围与演进

v1 用通用抽象跑通 VIP 工单提醒和周期报表两个场景；DSL 稳定之前不做画布。

1. **v1 通用内核**：全部 12 个模块的最小实现；DSL 用手写 yaml；节点类型限定为 trigger、condition、merge、timer、human\_choice、skill\_call、llm、lark\_card、send\_email、emit\_event、end；并发与故障注入测试覆盖分支、汇合、跳过传播、超时和点击与超时竞争。
2. **v2 AI 辅助配置**：基于模板（工单超时提醒、周期报表等 recipe）由 AI 生成定时任务和 workflow；dry-run 与审批流上线；建立 30 到 50 条真实需求的评估集。
3. **v3 可视化与扩展**：workflow 画布（设计态 + 运行态）；循环节点、子流程节点；运行时 LLM 增强步骤（如 RM 沟通建议）。

### 已确认

- **RM-VIP 关系**：通过 API 获取，一个 RM 对应多个 VIP 或一组 VIP。分配服务的关系解析器调用该 API 并带缓存；任务创建时快照 assignee；RM 交接时批量转派未完成任务。
- **源工单状态感知**：事件推送与定时扫描都需要。工单创建走事件推送；Maker Review Case 超过 48h、7 天这类持续条件走定时扫描；两条路径共用幂等键，不会重复建任务。
- **Skill schema**：Skillhub 中的 skill 已有机器可读的输入输出 schema，可以直接支撑 AI 生成定时任务和 workflow。
- **工作流引擎**：不使用 Temporal，自研轻量引擎，只依赖 TiDB 与 xxl-job，详见《轻量工作流引擎设计》。引擎是流程的事实来源，任务状态是其投影，经 `sync_task` effect 异步同步、最终一致。每个人工节点只由一个人决策，多人参与时拆成多个节点串联。
- **任务来源**：任务类型配置时需指定一个或多个来源（事件推送、cron 扫描、API），多个来源共用幂等键。cron 来源可由 AI 结合 skill、大数据 MCP、业务 DB MCP 生成；优先使用已认证的查询 skill，AI 编写的查询经审核后沉淀为新的 skill。
- **前后端交互**：前端经公司 gateway 调用 gRPC，接口用 `bgw.v1.method_options` 声明 HTTP 路径与限流；gateway 透传原始请求体与请求头，Lark 回调也走 gRPC，平台不部署 HTTP 服务。
- **版本管理**：任务类型为发布单元 + 自有/共享组件 + 发布清单；不提供「跟随最新版本」的绑定。
- **审批**：使用公司已有的审批平台，平台只保留版本状态与发布动作。
- **人员、组织与权限**：使用 BSP；前端选择器直接调用 BSP，接口级权限在 BSP 权限平台配置。配置带 `namespace` 字段，数据级权限暂不考虑。
- **Skillhub**：只有后端 API，前端的 skill 列表与 schema 由平台转发。
- **Kafka 来源**：控制反转，生产方按平台定义的 topic 与消息格式生产。
