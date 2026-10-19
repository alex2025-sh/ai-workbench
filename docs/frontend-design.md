# 前端交互设计

基于 `app/rpc/workbench.proto` 的 8 个 service，前端分为配置控制台、Workflow 画布、任务工作台三块。接口约定见 [ai-task-platform.md](ai-task-platform.md)「前后端交互」，任务类型 JSON 见 [task-type-spec.md](task-type-spec.md)。

## 一、结构与分期

| 模块 | 角色 | 服务 | 形态 |
| --- | --- | --- | --- |
| 配置控制台 | 平台管理员、配置开发者 | TaskTypeService、ComponentService、ChangeSetService、CatalogService | 独立站点，顶部导航 |
| Workflow 画布 | 配置开发者 | TaskTypeService（workflow 在草稿内）、CatalogService | 嵌入任务类型编辑页；运行态嵌入运行监控 |
| 任务工作台 | RM、CS、运营（处理人） | TaskService、RunService（只读进度） | demo 阶段与控制台同属一个前端应用，独立路由；与 Uni-Workbench 的集成后续再定 |

画布的设计态与运行态共用一个 React Flow 组件，只读与否、着色来源不同。

分期（与设计文档的 v1 到 v3 对齐，DSL 稳定前不做画布）：

| 期 | 内容 |
| --- | --- |
| demo | VIP 工单提醒端到端：我的任务、可认领、任务详情 + `Signal`（演示「已联系」「升级」）；运行监控只读（列表、节点状态表、事件时间线）；任务类型只读查看 + 试运行 |
| 一期 | 认领、开始处理、调整优先级、转派；运行监控的重试、跳过、取消；SLA 指示；变更集与差异页 |
| 二期 | 任务类型编辑：Monaco + JSON Schema 校验、Issue 面板、试运行、卡片预览、来源试跑；组件库；AI 生成草稿入口（草稿走同一编译器校验） |
| 三期 | 试运行路径叠加；语义化 workflow 差异。设计态画布（任务类型编辑）与运行态画布已提前实现 |

demo 不含变更集：变更集、审批后端尚未实现，demo 也没有草稿编辑器，页面没有数据。`sla_status` 还没有更新逻辑，放在一期。一期中认领、开始处理、调整优先级、转派与运行干预（取消运行、重试节点、跳过节点）已实现。

### demo 的运行方式

本地没有公司 gateway，浏览器不能直接调 gRPC，平台也不提供 HTTP 服务。demo 用一个**仅开发用**的 `cmd/devgw`：

- 按 proto 方法行尾注释的路径，把 `POST /byai_platform/api/v1/...` 路由到对应 gRPC 方法，请求与响应用 protojson 转换。
- 把请求头 `X-Dev-User` 写入 gRPC metadata，模拟 gateway 的身份注入。
- 前端顶部放「切换身份」下拉（RM、候选组成员、运维、管理员），身份取自 `deploy/mock` 的人员关系；BSP 选人与姓名解析在 demo 中也读这份 mock。

- 错误返回 `{"code": "<gRPC 状态码名>", "message": "..."}`，HTTP 状态码与 grpc-gateway 的默认映射一致（如 `NotFound` → 404、`Unauthenticated` → 401、`Unimplemented` → 501）；`Signal` 被拒绝不是错误，HTTP 200 + `accepted=false`。
- 开发专用的 `GET /dev/users` 列出 mock 中的人员、角色与所属组，供「切换身份」使用。

启动：`go run ./app/rpc -f app/rpc/etc/workbench.yaml` 与 `go run ./cmd/devgw`（默认 `127.0.0.1:8088`）。gateway 接入后 devgw 不再使用，前端只换 base URL。

## 二、路由

| 路由 | 页面 | 主服务 |
| --- | --- | --- |
| `/tasks` | 我的任务（含「可认领」Tab） | TaskService |
| `/tasks/:id` | 任务详情，Lark 卡片「在工作台打开」的深链 | TaskService |
| `/runs` | 运行监控列表 | RunService |
| `/runs/:id` | 运行详情（运行态画布、节点详情、事件时间线） | RunService |
| `/config/task-types` | 任务类型列表 | TaskTypeService |
| `/config/task-types/:key/edit` | 任务类型编辑（含画布） | TaskTypeService、CatalogService |
| `/config/components` | 组件库 | ComponentService |
| `/config/changesets` | 变更集列表 | ChangeSetService |
| `/config/changesets/:id` | 变更集详情 | ChangeSetService |
| `/config/changesets/:id/diff` | 版本差异，**独立只读页**，审批单里放这个链接，审批人未必有配置权限 | ChangeSetService |

`IngestService`、`CallbackService` 不由前端调用；`CatalogService` 只在画布和表单中间接使用。

## 三、任务工作台

### 1. 我的任务 `/tasks`

```text
[我的任务] [可认领]                                                    [刷新]
状态 chip（多选）  优先级 chip
P0  待处理 1 · 处理中 2
┌───────────────────────────┐ ┌───────────────────────────┐ ┌──────────────
│▌[P0][处理中] 标题…   已逾期 │ │▌[P0][待处理] 标题…        │ │ …
│ 任务类型 · 截止 10-19 12:23 │ │ 任务类型 · 截止 10-20 10:23 │
│ 卡片摘要（两行截断）        │ │ 卡片摘要                   │
│ [查看][开始处理][选项…][🔗] │ │ [查看][开始处理][选项…][🔗] │
└───────────────────────────┘ └───────────────────────────┘
                         查看更多（还有 n 个）
P1  待处理 120 · 处理中 10
…
```

| 动作 | 接口 |
| --- | --- |
| 进入、筛选 | 每个优先级各调一次 `ListMyTasks({statuses, priorities: [P], page})` |
| 「可认领」Tab | 同上，`ListClaimableTasks({priorities: [P], page})` |
| 分区「查看更多」 | 该分区带 `next_cursor` 再查一页 |
| 卡片：认领 / 开始处理 | `ClaimTask` / `StartProcessing` |
| 卡片：流程选项 | `Signal`（`run_id` 取自摘要，`node_id`、`iteration`、选项取自摘要的 `pending_action`） |

- **按优先级分区的卡片网格。** 每个优先级是一个分区，分区各自查询、各自游标分页；分区标题的计数来自该分区第一页返回的 `counts`（按 `(priority, status)` 分组、条件与请求相同、不受分页影响），所以是准确的。没有任务的分区不显示。卡片三列（≥1200px）、两列（≥768px）、一列。
- **卡片内容。** 标题行：优先级、状态、标题；`due_at` 已过且未完成时右上角显示红色「已逾期」角标。正文：任务类型名称（`task_type_name`）· 截止或推迟至，加上卡片摘要（`subtitle`，两行截断）。标题与摘要取任务当前显示的卡片（与详情页的 `rendered_json` 同一张，如升级后显示升级卡片），已按 `x-pii` 脱敏，前端不解析 payload。点卡片空白处进入详情。
- **卡片上的操作。** 「查看」；主按钮为「认领」（待认领）或「开始处理」（待处理且自己是处理人）；然后是等待中人工节点的选项（`pending_action.options`，仅 `can_signal` 时显示），最多直接显示两个，其余收进「更多」；最后是复制链接。选项的提交方式：
  - 有 `required_fields` 的选项打开与详情页相同的表单确认框；
  - 推迟选项（`snooze_ms` 非 0）一键提交，时长在悬停提示里；
  - 其余选项气泡二次确认后提交。

  按钮等宽，文字放不下时省略、悬停看全文。
- **提交之后。** 任务表由引擎异步同步，卡片操作成功后约 0.8 秒、3 秒各重新加载一次所在分区（认领后任务会移到「我的任务」）。被拒绝（已在 Lark 上处理、卡片过期）时提示原因并刷新。
- SLA：`on_track` 绿、`at_risk` 黄、`breached` 红；后端更新 `sla_status` 之前（一期）不显示。逾期角标只看 `due_at`。
- `assignee_id` 经 BSP 批量解析成姓名并缓存。

### 2. 任务详情 `/tasks/:id`

```text
← 返回   {title}
状态  优先级  处理人  SLA  截止  推迟至
┌──────────────────────────┬──────────────────────────────────────┐
│ 任务卡片（rendered_json） │ 操作                                  │
│ 字段、标签、格式由展示     │ [认领] [开始处理] [调整优先级] [转派]   │
│ 模板决定，已脱敏          │ ── 流程操作（pending_action）──        │
│                          │ 当前节点：ask_rm                       │
│ 事件时间线（TaskEvent）   │ [已联系] [推迟] [升级]                  │
│                          │ 进度：○ 通知 ● 等待RM ○ 结束            │
└──────────────────────────┴──────────────────────────────────────┘
```

| 动作 | 接口 |
| --- | --- |
| 进入 | `GetTask({id})` → `TaskDetail{task, rendered_json, pending_action, progress}` |
| 时间线 | `GetTaskEvents` |
| 认领、开始处理、调整优先级、转派 | `ClaimTask`、`StartProcessing`、`UpdatePriority`、`Reassign`（直接走任务中心，同步生效） |

任务中心操作的规则与权限（接口级权限线上来自 BSP，本地见 `deploy/mock/permissions.yaml`）：

| 操作 | 谁可以 | 规则 |
| --- | --- | --- |
| 认领 | 组队列的成员 | 只有 `UNASSIGNED` 且在组队列中的任务；并发认领只有一个成功，其余 `FailedPrecondition` |
| 开始处理 | 处理人 | `PENDING` → `PROCESSING` |
| 调整优先级 | 处理人，或有 `task.admin` | 未完结；原因必填 |
| 转派 | 处理人，或有 `task.admin` | 未完结；原因必填；只改变处理人、状态不变（待认领的任务转派后变为 `PENDING`） |

认领或转派之后，引擎同步过来的状态不会把任务退回待认领，也不会覆盖处理人；只有流程再次明确分配处理人（如进入下一个人工节点）时才会覆盖。
| 流程操作 | `Signal({run_id, node_id, iteration, option, form})`，`run_id` 取自 `task.run_id`，`node_id`、`iteration`、选项取自 `pending_action` |

`RunService.Signal` 是人工操作唯一的对外入口，服务端按任务表鉴权，与 Lark 卡片回调共用同一个鉴权函数（引擎本身不鉴权）。

要点：

- **字段用 KV 表格展示。** 卡片的 `fields` 渲染为带边框的两列 KV 表格（窄屏一列）：标签列浅底、宽度固定、次要色，值列左对齐、长文本换行；`short=false` 的字段独占一行，一行只剩一个短字段时占满整行；空值显示「—」。字段的顺序与标签由展示模板决定。
- **任务卡片不写死字段。** 前端只渲染 `rendered_json`，与配置端 `PreviewView(channel=web)` 共用同一个渲染组件。脱敏值由后端处理，前端不判断。`rendered_json` 是最近一个发卡片的节点**实际发出的那张卡片**（记在节点输出里），与 Lark 上看到的一致，不按之后变化的状态重新渲染。
- **流程操作按钮由 `pending_action.options` 生成。** `can_signal=false` 时只读展示。`can_signal` 为真的条件：调用人是处理人，或任务未分配且调用人在候选人 / 候选组内。**组任务不需要先认领就能做决定**，做决定的人成为处理人；认领只是「先占住、避免别人重复处理」。
- **表单与 DSL 一一对应。** 表单字段在节点上、所有选项共用（`pending_action.form_fields`，类型为 text、textarea、select 等）；每个选项只声明 `required_fields`。前端用一个固定的小表单组件渲染，点击某个选项时校验它的必填字段，填写值作为 `form`（字符串 map）提交。不需要 JSON Schema 表单库。
- **推迟。** `snooze_ms` 非 0 的选项是推迟，时长由配置决定，用户不可选。推迟后任务为「已推迟」并显示推迟至的时间，到期重新提醒；推迟期间仍可直接选择其他选项。
- **同一时刻至多一个等待中的人工节点。** 多人参与时人工节点串联（单人决策），所以 `pending_action` 是单值。
- **进度只读简化。** 用 `progress` 画一行步骤，不跳转运行监控；完整运行页只给运维。
- **按钮可用性**：

| 状态 | 认领 | 开始处理 | 调优先级 / 转派 | 流程操作 |
| --- | --- | --- | --- | --- |
| UNASSIGNED | ✓（调用人在候选范围内） | | ✓ | 有 `pending_action` 且 `can_signal`（候选范围内可直接决定） |
| PENDING | | ✓ | ✓ | 有 `pending_action` 且 `can_signal` |
| PROCESSING | | | ✓ | 同上 |
| SNOOZED | | | ✓ | 同上 |
| SOLVED | | | | |

#### Signal 的结果处理

`Signal` 返回后任务表是**异步**同步的（`sync_task`），所以：

1. `accepted=true`：关闭表单，乐观地把按钮置灰、显示「处理中」，然后每 2 秒短轮询 `GetTask`，直到 `pending_action` 的 `(node_id, iteration)` 变化或 `task.updated_at` 变化，最多约 20 秒，超时提示刷新。只比较 `node_id` 不够：受控环中同一节点会进入下一轮。
2. `accepted=false`，按 `code` 区分：

| code | 处理 |
| --- | --- |
| `already_handled`、`stale`、`not_waiting` | 提示「已被处理」，直接刷新详情（通常是 Lark 卡片先点了） |
| `invalid_form` | 留在表单，显示 `reason` |
| `unsupported` | 提示该操作暂不可用 |
| `forbidden` | 提示无权限，刷新 |

### 3. 运行监控 `/runs`、`/runs/:id`

| 动作 | 接口 |
| --- | --- |
| 列表 | `ListRuns({task_id, biz_key, status, task_type_key, created_after, created_before, page})`；需人工介入的 run 带 `blocked_node`、`blocked_reason` |
| 详情 | `GetRun`（`nodes[]` 含 `last_error`、`graph_json`、`ui_json` 中的画布坐标、`blocked_node` / `blocked_reason`） |
| 事件时间线 | `GetRunEvents` |
| 重试、跳过、取消 | `RetryNode`、`SkipNode`（选端口并填原因）、`CancelRun`；均需 `run.admin`，二次确认 + 必填原因 |

- 重试：run 为「需人工介入」时，失败的节点，以及卡片发送失败而仍在等待的人工节点显示「重试」。
- 跳过：就绪、执行中、等待中或失败的节点显示「跳过」，出口取自 `graph_json` 中从该节点连出的边。
- 取消后进行中的节点状态为 `cancelled`，显示「已取消」。语义见 `docs/workflow-engine.md`「运行干预」。

- 列表顶部放「需人工介入」快捷过滤和数量角标：`needs_intervention` 目前不同步到任务中心，不看这里没人会发现。
- 列表显示需人工介入的 run 卡在哪个节点、原因，不用点进详情。
- 详情以运行态画布为主：按该 run 锁定版本的 workflow 绘制（坐标沿用设计时保存的 `ui.canvas`，没有时自动排版），节点按 `NodeView.status` 着色，未到达的节点淡化，走过的连线高亮；初始视野居中到卡住或正在等待的节点。分支节点在卡片上写出口，执行超过一次时写「执行 n 次」，不单独列出口与执行次数。
- 需人工介入时页头显示「卡在节点 X」与原因，并直接给出重试、跳过按钮；点击节点在右侧看状态、起止时间、最近一次错误（重试成功后仍保留，便于排查）与干预按钮。
- 事件时间线把事件翻译成一句话（如「第 2 次执行失败，10:05 重试：…」），失败类标红，原始 payload 可展开。
- 刷新用按钮，详情页打开期间 running 的 run 每 5 秒轮询一次。

## 四、配置控制台

### 4. 任务类型列表 `/config/task-types`

`ListTaskTypes`、`CreateTaskType`、`ListTaskTypeVersions`。`key` 仅允许 `[a-z0-9_]`；`has_draft` 显示「编辑中」，`retired` 置灰。namespace 筛选放在全局头部并记住上次选择。

### 5. 任务类型编辑 `/config/task-types/:key/edit`

左侧 Tab：基本信息、Schema、来源、Workflow、卡片模板、分配策略、SLA。右侧内容区。**整份草稿是一份 `body_json`、一个 `revision`**，各 Tab 共享同一份前端状态。

| 动作 | 接口 |
| --- | --- |
| 进入 | `GetTaskTypeDraft`（无草稿时后端基于最新版本创建）；只读查看已发布版本用 `GetTaskType` |
| 保存 | `SaveTaskTypeDraft({key, revision, body_json})` → 新 revision + 快速 `issues` |
| 校验 | `ValidateTaskTypeDraft`（校验的是**已保存**草稿） |
| 试运行 | `GenerateSamplePayload` 或手填 → `DryRunTaskTypeDraft` |
| 卡片预览 | `PreviewView`，未保存的模板用 `inline_view_json` |
| 来源试跑 | `TrialSource` |
| 丢弃 | `DiscardTaskTypeDraft` |
| 发布 | 「加入变更集」→ 跳 `/config/changesets/:id` |

保存与校验：

- 编辑后 800ms 防抖自动保存，用 `SaveTaskTypeDraft` 返回的 `issues` 作为实时反馈；「校验」按钮才调完整校验。
- **保存串行**：同一时刻只有一个保存请求在途，期间的修改合并成最后一份，等上一个返回、拿到新 revision 后再发；否则自己的两次保存会撞上乐观锁。
- Monaco 中 JSON 语法不合法时不保存，只在编辑器内提示。
- 节点拖动、缩放只改 `ui` 坐标，不触发校验，合并到下一次自动保存。`ui` 不计入 `content_hash`，所以不会让试运行结果失效。

冲突与锁定：

- `revision` 冲突：**保留本地副本**，弹窗给三个选择：「查看对方改动」（本地与服务端 JSON 对比）、「导出我的 JSON」、「以服务端为准重新加载」。不直接刷新，避免丢掉未保存的画布修改。
- `locked_by_change_set ≠ 0`：全部只读，顶部提示「已加入变更集 #N，审批中」，附「去变更集」和「撤回」入口。

Issue 面板（底部抽屉，各页共用）：

| `path` 前缀 | 跳转 |
| --- | --- |
| `workflow.` | Workflow Tab，按 `node_id`、`edge_id` 高亮 |
| `schema.` | Schema Tab |
| `source.` | 来源 Tab |
| `view.` | 卡片模板 Tab |
| `assignment.` | 分配策略 Tab |
| `sla.` 与其他 | 对应 Tab，找不到则停留在面板 |

`error` 红色，阻止发布；`warning` 黄色，不阻止。

试运行：结果 `DryRunResp.cases[]`，每个样本一份。

- 路径（`path`）：二期用列表展示，三期叠加到画布上，走过的节点和边高亮。
- 处理人、模拟发送清单（`sends`，点开看渲染后的卡片或邮件）、超时与升级时间点（`timeline`，相对起点的偏移）。
- `DryRunResp.content_hash` 与草稿当前 `content_hash` 不一致时，标记「结果已过期」。比较哈希而不是 revision：只挪画布坐标不算改动。

### 6. 任务类型编辑与 Workflow 画布

范围：新建任务类型；编辑页的基本信息（表单）、流程（画布，节点配置按 schema 生成表单）、字段 / 来源 / 卡片模板 / 处理人（暂用 JSON 编辑）；草稿自动保存、校验、对草稿试运行、丢弃；开发环境直接发布。不含：退回边（受控环）的创建（已有的能显示）、共享组件 `key@version`、变更集与审批、AI 生成、卡片预览、来源试跑。

```text
┌ ← 任务类型  VIP 工单提醒 (vip_appeal_alert)  草稿 · 基于 v1 · 已保存   [校验] [试运行] [丢弃草稿] [发布] ┐
├────────┬───────────────────────────────────────────────────────────────────────────┤
│ 基本信息 │ ┌ 节点 ────┐ ┌ 画布 ──────────────────────────────┐ ┌ 节点配置 ──────┐ │
│ 流程     │ │ 原生      │ │ [开始]→[通知RM]→[等待48h]→[查状态]→◇ │ │ escalate       │ │
│ 字段     │ │ 动作      │ │                                    │ │ 表单 | JSON    │ │
│ 来源     │ └──────────┘ └────────────────────────────────────┘ └───────────────┘ │
│ 卡片模板 │ ┌ 校验结果（点击定位）─────────────────────────────────────────────┐   │
│ 处理人   │ └──────────────────────────────────────────────────────────────────┘   │
└────────┴───────────────────────────────────────────────────────────────────────────┘
```

画布交互：

- 从左侧面板（`ListNodeTypes`，按 `category` 分组）拖入节点，或点击加在当前视野中央，自动生成 id（如 `timer_2`）；id 可在配置面板修改，引用它的连线随之更新。
- 端口由节点类型与配置决定（规则同 `dsl.Ports`）：条件分支每个分支一个、外加 `default`；人工选择每个非推迟选项一个，配置超时另有 `timeout`；其他为固定的 `next` / `error`。改配置后端口随之变化，已连线的端口被删掉时该连线标红。
- 从端口拖到节点即连线，同一端口可连多个节点（并行）；Delete 删除选中的节点或连线。
- 坐标存在任务类型 JSON 的 `ui.canvas`（编译器忽略）；没有坐标时（如 YAML 发布的示例）首次打开自动排版（dagre）。
- 节点卡片显示类型、名称与一行摘要（定时「48h」、人工节点的选项）。
- 校验问题按 `node_id` / `edge_id` 描红，点击底部问题选中节点并打开配置。

节点配置表单：

- 后端 `ListNodeTypes` 返回每种节点的配置 schema（与校验用的是同一份 `task_type.schema.json`），前端用自研的渲染器生成表单：schema 用 `if/then` 区分类型（如处理人解析器），部分字段要从当前任务类型里选值，通用 JSON Schema 表单库处理不好。
- 控件按 `$ref` 的定义名选择：时长 `duration`（`48h`，带格式校验）、表达式 `expression`（等宽）、模板 `template`、卡片模板 `view_binding`（当前任务类型的 views）、skill `skill_name`（`ListSkills`）、处理人 `assignment_binding`（先选解析器类型，再出对应字段）、参数映射 `template_map`（键值对）。
- 中文标签按字段名取（`web/src/components/schemaform/labels.ts`），字段说明用 schema 的 `description`；标签属于展示层，不写进 schema。`ui_schema_json` 暂为空。
- 每个节点都可以切到 JSON 编辑，改的是同一份数据。

保存、校验、试运行、发布：

| 动作 | 接口 | 说明 |
| --- | --- | --- |
| 打开 | `GetTaskTypeDraft` | 没有草稿时基于最新发布版本创建 |
| 自动保存 | `SaveTaskTypeDraft` | 防抖 800ms、串行、乐观锁；返回快速校验结果；冲突时保留本地修改并让用户选择 |
| 校验 | `ValidateTaskTypeDraft` | 完整编译，结果在底部面板 |
| 试运行 | `DryRunTaskTypeDraft` | 针对草稿；`content_hash` 不含 `ui`，只拖动坐标不让结果失效 |
| 丢弃 | `DiscardTaskTypeDraft` | 确认后回到已发布版本（新建且未发布的类型回到列表） |
| 发布 | `PublishTaskTypeDraft`（仅开发环境） | 有错误时拒绝；开关 `AllowDirectPublish` + 权限 `task_type.publish`；正式发布走变更集与审批 |
| 新建 | `CreateTaskType` | 填 key、名称、命名空间、描述，生成开始 → 结束、空字段 schema、默认卡片模板的草稿 |

### 7. 组件库 `/config/components`

四个 Tab 对应 `ConfigKind`（卡片模板、分配策略、跨部门协议、提示词模板）。接口：`ListComponents`、`CreateComponent`、`GetComponentDraft`、`SaveComponentDraft`、`DiscardComponentDraft`、`ValidateComponentDraft`、`ListComponentVersions`、`ListDependents`。引用方侧栏标注 `is_latest`，并提供「加入变更集并升级引用方」入口（`AddDependentUpgrades`）。草稿编辑模式与任务类型一致。

### 8. 变更集 `/config/changesets`、`/:id`、`/:id/diff`

| 动作 | 接口 |
| --- | --- |
| 列表 | `ListChangeSets`，状态 Tab：全部、open、submitted、published、rejected、withdrawn |
| 创建、详情 | `CreateChangeSet`、`GetChangeSet` |
| 升级引用方 | `AddDependentUpgrades` |
| 差异 | `DiffChangeSet` |
| 提交、撤回 | `SubmitChangeSet`（返回审批链接，新窗口打开）、`WithdrawChangeSet` |

- 每个 item 显示试运行状态：`dry_run_required` 且未 `dry_run_passed` 时标「未试运行」，**「提交审批」禁用**，并给出「去试运行」入口。`dry_run_passed` 按内容哈希判定，不设有效期。
- `issues` 中有 `error` 时「提交审批」同样禁用。
- 差异页：对 `base_json`、`target_json` 先**去掉 `ui` 字段**再做结构化 diff（jsondiffpatch），避免画布坐标淹没真实改动；workflow 部分按节点、边、字段分组显示新增、删除、修改。三期加画布上的可视化差异。
- 审批回调由审批平台调用，前端不处理；详情页打开期间 submitted 的变更集每 10 秒轮询一次。

## 五、全局约定

### 草稿编辑

```text
打开 → Get*Draft → locked? 只读 : 可编辑
编辑 → 防抖 Save*Draft(revision) → 成功: 更新 revision + issues
                                 → 冲突: 保留本地副本，三选一
```

### 分页

所有列表为游标分页：首次 `cursor=""`、`page_size=20`，之后带上次的 `next_cursor`，为空则隐藏「加载更多」。

### proto 与 gateway JSON 的前端约定

API 客户端层统一处理，业务代码不接触原始编码。以下按 proto3 标准 JSON 映射（protojson，devgw 即按此实现）编写，**公司 gateway 的实际行为待确认**；如有差异只改客户端层：

- 字段名为 proto 中的 snake_case（`run_id`、`pending_action`），零值字段也会输出（devgw 使用 `UseProtoNames`、`EmitUnpopulated`）。
- `bytes` 字段在 JSON 中是 base64，客户端封装 `decodeJson`、`encodeJson`；金额等字段保持字符串，解析用 `json-bigint` 或保留字符串，不转 number。
- `int64`（`id`、`run_id`、`revision`、所有时间戳）在 JSON 中是**字符串**，前端一律按字符串处理和比较；时间戳是 Unix 毫秒。
- 枚举用名字（`TASK_STATUS_PENDING`），不要使用数字；`TaskStatus` 的数值与存储值偏移 2，前端不依赖。
- 错误：冲突、权限、校验失败的 gRPC 状态码与前端提示的对应关系在客户端层集中映射；`SaveTaskTypeDraft` 的 `issues` 不算错误。

### 身份与权限

- 人员、用户组选择器直接调用 BSP；平台返回的 ID（`assignee_id`、`created_by`、`approved_by`）批量经 BSP 解析姓名并缓存。
- 按钮显示（重试、跳过、提交发布等）以 BSP 的接口权限为准，无权限时隐藏入口；真正的校验在后端。

### 技术选型

| 层面 | 方案 |
| --- | --- |
| 画布 | React Flow |
| 节点配置表单（二期起） | JSON Schema 驱动（react-jsonschema-form 或 formily）；任务详情的人工节点表单是固定小组件，不用它 |
| JSON 编辑器 | Monaco Editor（任务类型 JSON、payload 手输、试运行样例） |
| JSON 差异 | jsondiffpatch |
| 状态管理 | zustand 或 jotai（草稿、画布） |
| 通信 | HTTP POST + JSON，经统一客户端层；demo 指向 devgw，之后指向 gateway |

## 六、已确认与暂缓

| 事项 | 结论 |
| --- | --- |
| `ListClaimableTasks` 的候选范围 | 调用人在候选人或候选组内的 `unassigned` 任务 |
| 试运行结果 | 不设有效期；`dry_run_passed` 只在草稿 revision 变化后重置 |
| 与 Uni-Workbench 集成 | demo 先做成同应用的独立路由，用 `/tasks/:id` 作为 Lark 卡片深链；正式接入方式（微前端或组件库）后续再定，路由与接口层不受影响 |
| 指标看板 | 暂缓，后续再做；目前没有对应接口 |
| gateway 的 JSON 映射 | 待确认：`bytes`、`int64`、枚举的表示方式；demo 先按 protojson |
| `needs_intervention` | 见下 |

### `needs_intervention`

run 的状态之一（`running / succeeded / failed / needs_intervention / cancelled`），表示**引擎无法自动继续，需要运维人工处理**。run 不会自行恢复，也不会结束，任务中心里的任务状态不变。触发场景：

| 场景 | 说明 |
| --- | --- |
| 节点重试耗尽且没有 `error` 端口 | 外部调用（skill、LLM、邮件等）持续失败 |
| 毒丸 job | 加载定义失败、配置无法解码等确定性错误，连续出错 5 次后隔离该 job |
| 卡片发送失败 | Lark 不可用，退避重试 10 次（约 30 分钟）仍失败，写 `effect_failed` 事件 |
| 触达总量超限 | 节点总执行次数或运行总时长超过上限（受控环的兜底） |

运维在运行监控中处理：重试该节点、跳过（指定端口）或终止 run。所以前端只在运行监控里做提醒（列表置顶「需人工介入」过滤与数量角标），不通知处理人；是否同步到任务中心仍待产品决策。

## 七、后端配套改动

前端依赖、但不在本文范围内实现的后端改动，单独开分支进行：

| 改动 | 说明 |
| --- | --- |
| `cmd/devgw` | 开发用 HTTP 到 gRPC 的转发与身份注入（见「demo 的运行方式」） |
| `RunService.Signal` 鉴权 | 按任务表鉴权后调引擎；鉴权函数与 `LarkCardCallback` 共用 |
| 引擎返回结构化拒绝原因 | `SignalResult` 目前只有中文 `reason`，增加 `code`（`already_handled`、`stale`、`not_waiting`、`invalid_form`、`unsupported`） |
| 列表标题 | 查询时用默认模板渲染，不落库（已实现） |
| 草稿 `content_hash` | 去掉 `ui` 后计算；试运行记录该哈希，变更集据此判定 `dry_run_passed` |
| 编译器禁止并行人工节点 | `pending_action` 为单值的前提；目前没有这条校验 |
