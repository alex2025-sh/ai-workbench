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
| 三期 | React Flow 画布：设计态与运行态；试运行路径叠加；语义化 workflow 差异 |

demo 不含变更集：变更集、审批后端尚未实现，demo 也没有草稿编辑器，页面没有数据。任务中心的 `Claim`、`StartProcessing`、`UpdatePriority`、`Reassign` 目前返回未实现，`sla_status` 也没有更新逻辑，所以这些放在一期。

### demo 的运行方式

本地没有公司 gateway，浏览器不能直接调 gRPC，平台也不提供 HTTP 服务。demo 用一个**仅开发用**的 `cmd/devgw`：

- 按 proto 方法行尾注释的路径，把 `POST /byai_platform/api/v1/...` 路由到对应 gRPC 方法，请求与响应用 protojson 转换。
- 把请求头 `X-Dev-User` 写入 gRPC metadata，模拟 gateway 的身份注入。
- 前端顶部放「切换身份」下拉（RM、候选组成员、运维、管理员），身份取自 `deploy/mock` 的人员关系；BSP 选人与姓名解析在 demo 中也读这份 mock。

gateway 接入后 devgw 不再使用，前端只换 base URL。

## 二、路由

| 路由 | 页面 | 主服务 |
| --- | --- | --- |
| `/tasks` | 我的任务（含「可认领」Tab） | TaskService |
| `/tasks/:id` | 任务详情，Lark 卡片「在工作台打开」的深链 | TaskService |
| `/runs` | 运行监控列表 | RunService |
| `/runs/:id` | 运行详情（时间线；三期加运行态画布） | RunService |
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
[我的任务] [可认领]
状态 chip（多选）  优先级 chip  任务类型 ▾                    [刷新]
┌─────────────────────────────────────────────────────────────────┐
│▌P0  VIP 工单提醒 · 张三 工单 T-1234    待处理   截止 10-19  ⚠ at_risk │
│▌P0  ...                                                          │
│▌P1  ...                                                          │
└─────────────────────────────────────────────────────────────────┘
                              [加载更多]
```

| 动作 | 接口 |
| --- | --- |
| 进入、筛选 | `ListMyTasks({statuses, task_type_key, priorities, page})` |
| 「可认领」Tab | `ListClaimableTasks({task_type_key, priorities, page})` |
| 认领 | `ClaimTask`，成功后行移到「我的任务」 |

- 返回 `TaskSummary`，标题与副标题由服务端用任务类型默认模板的 `title` / `summary` 渲染（只用 payload 与任务字段，按 `x-pii` 脱敏，不区分查看人），前端不解析 payload。
- 后端按优先级、`due_at` 排序，前端**平铺**显示并用左侧色条区分 P0 到 P3，不做分组折叠：游标分页下各组计数不准，P0 可能出现在第二页。要看某一优先级用筛选。
- SLA：`on_track` 绿、`at_risk` 黄、`breached` 红；后端更新 `sla_status` 之前（一期）不显示该列。
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
| 认领、开始处理、调整优先级、转派 | `ClaimTask`、`StartProcessing`、`UpdatePriority`、`Reassign`（直接走任务中心，同步生效；一期） |
| 流程操作 | `Signal({run_id, node_id, iteration, option, form})`，`run_id` 取自 `task.run_id`，`node_id`、`iteration`、选项取自 `pending_action` |

`RunService.Signal` 是人工操作唯一的对外入口，服务端按任务表鉴权，与 Lark 卡片回调共用同一个鉴权函数（引擎本身不鉴权）。

要点：

- **任务卡片不写死字段。** 前端只渲染 `rendered_json`，与配置端 `PreviewView(channel=web)` 共用同一个渲染组件。脱敏值由后端处理，前端不判断。`rendered_json` 是最近一个发卡片的节点**实际发出的那张卡片**（记在节点输出里），与 Lark 上看到的一致，不按之后变化的状态重新渲染。
- **流程操作按钮由 `pending_action.options` 生成。** `can_signal=false` 时只读展示。`can_signal` 为真的条件：调用人是处理人，或任务未分配且调用人在候选人 / 候选组内。**组任务不需要先认领就能做决定**，做决定的人成为处理人；认领只是「先占住、避免别人重复处理」。
- **表单与 DSL 一一对应。** 表单字段在节点上、所有选项共用（`pending_action.form_fields`，类型为 text、textarea、select 等）；每个选项只声明 `required_fields`。前端用一个固定的小表单组件渲染，点击某个选项时校验它的必填字段，填写值作为 `form`（字符串 map）提交。不需要 JSON Schema 表单库。
- **推迟。** `snooze_ms` 非 0 的选项是推迟，时长由配置决定，用户不可选。引擎目前拒绝推迟，demo 中这类按钮置灰并提示「即将开放」。
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
| 列表 | `ListRuns({task_id, biz_key, status, task_type_key, created_after, created_before, page})` |
| 详情 | `GetRun`（`nodes[]`、`graph_json`） |
| 事件时间线 | `GetRunEvents` |
| 重试、跳过、取消 | `RetryNode`、`SkipNode`（选端口并填原因）、`CancelRun`；均需二次确认 + 必填原因 |

- 列表顶部放「需人工介入」快捷过滤和数量角标：`needs_intervention` 目前不同步到任务中心，不看这里没人会发现。
- 一期详情是节点状态表加时间线；三期换成运行态画布，按 `NodeView.status` 着色。
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

### 6. Workflow 画布（三期）

- 节点面板：`ListNodeTypes` 按 `category` 分组。
- 配置面板：用 `config_schema_json` + `ui_schema_json` 渲染表单；skill 下拉用 `ListSkills`，参数表单用 `GetSkillSchema`；共享组件下拉用 `ListComponents`，存为 `key@version`。
- 端口：按 `port_rule`——`fixed` 取 `fixed_ports`，`from_options` 由 human_choice 的选项生成，`from_branches` 由 condition 的分支生成。
- 共享组件有新版本时，在节点上提示「可升级到 vN」：前端用 `GetTaskType` 的 `manifest.ref_version` 对比 `ComponentSummary.latest_version`。

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
