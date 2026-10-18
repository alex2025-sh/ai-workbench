# 任务类型 JSON 说明

前端画布、仓库中的 YAML、AI 生成的配置，最终都是同一份任务类型 JSON，作为 `SaveTaskTypeDraft.body_json` 提交。本文说明它的结构；字段的精确约束以 [`api/schema/task_type.schema.json`](../api/schema/task_type.schema.json) 为准，完整示例见 [`examples/task_types/vip_appeal_alert.yaml`](../examples/task_types/vip_appeal_alert.yaml)。

## 校验分三层

| 层次 | 检查什么 | 在哪里 |
| --- | --- | --- |
| 结构 | 字段名、类型、必填项、取值范围 | `task_type.schema.json`，前端也可以直接用它做本地校验 |
| 语义 | payload schema、来源映射、去重键、卡片模板及其引用 | `internal/spec` |
| workflow | 连线、端口、环、变量引用、view / assignment 继承 | `internal/flow/compiler`，同时产出执行计划 |

保存草稿时返回快速校验结果但不阻止保存；提交变更集和发布时，存在 error 级别的问题会被拒绝。

## 顶层结构

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `key` | 是 | 任务类型标识，`^[a-z][a-z0-9_]{1,63}$`，创建后不可修改 |
| `namespace` | | 所属团队或业务域 |
| `name` | 是 | 显示名称 |
| `description` | | 任务说明，同时作为 AI 摘要的场景说明 |
| `default_priority` | | `P0`–`P3`，默认 `P2` |
| `schema` | 是 | payload 的 JSON Schema，顶层必须是 object |
| `sources` | | 任务来源：`cron` / `kafka` / `api` |
| `dedup_key` | | 去重键模板，如 `{{ payload.appeal_id }}` |
| `views` | | 卡片模板：名称 → 内嵌模板，或共享组件 `key@version` |
| `view` / `assignment` | | 默认卡片模板与分配策略，节点未配置时继承 |
| `workflow` | 是 | `nodes` + `edges` |
| `ui` | | 前端专用（画布坐标等），后端原样保存，编译器忽略 |

## payload schema 的扩展字段

字段的 `title`、`description` 同时用于表单标签、卡片字段名和 AI 摘要的字段说明。

| 关键字 | 取值 | 作用 |
| --- | --- | --- |
| `x-pii` | `mask_middle` / `hash` / `drop` | 发往 Lark、LLM、日志时统一脱敏 |
| `x-format` | `money` / `percent` / `duration` / `datetime` | 展示格式；`money` 字段的类型必须是 string |
| `x-currency-field` | 同级字段名 | 金额对应的币种字段 |
| `x-enum-labels` | 值 → 中文含义 | 展示与给 LLM 的上下文 |
| `x-enum-source` | 字典名 | 值来自外部字典 |

## 表达式与模板

- **表达式**：`when`、`anchor`、`filter`、`close_when`、`subject`、`expr`、`exclude`、来源的 `mapping` 直接写表达式，如 `payload.vip_level > 0`。
- **模板**：其余字符串都是模板，用 `{{ 表达式 }}` 插值，如 `VIP 工单 {{ payload.appeal_id }}`。

语法是 [expr-lang](https://expr-lang.org/docs/language-definition)。表达式是纯函数，不能调用 `now()`。

| 命名空间 | 内容 | 可用位置 |
| --- | --- | --- |
| `payload` | 任务 payload | workflow、卡片模板、去重键、默认分配策略 |
| `task` | 任务公共字段：`id`、`priority`、`assignee` | workflow、卡片模板、默认分配策略 |
| `nodes.<id>.output` | 上游节点的输出 | workflow（节点必须在当前节点上游）、卡片模板 |
| `loop.iteration` | 退回环内的轮次 | 环内节点 |
| `record` | 扫描结果或消息中的原始记录 | 来源的 `filter`、`close_when`、`mapping` |
| `viewer` | 查看者：`dept`、`roles` | 卡片的受众变体 `variants[].when` |

## 节点类型与端口

| 节点 | 端口 | 主要配置 |
| --- | --- | --- |
| `trigger` | `next` | 无，入口，每个 workflow 恰好一个 |
| `condition` | 每个分支的 `port` + `default` | `branches: [{ port, when }]`，按顺序选中第一个为 true 的分支 |
| `merge` | `next` | `mode: all / any` |
| `timer` | `next` | `after`（如 `48h`、`7d`），可选 `anchor`（从某个时间字段开始算） |
| `human_choice` | 每个选项的 `key`；配置 `timeout` 时另有 `timeout` | `options`、`view`、`assignment`、`form`、`timeout`、`remind`；带 `snooze` 的选项表示推迟，不产生端口 |
| `skill_call` / `llm` / `lark_card` / `send_email` / `emit_event` | `next`、`error` | 见 schema；`error` 端口可以不连，失败时进入人工干预 |
| `end` | 无 | 可选 `resolution` |

除 `error` 外，每个端口都必须有连线。

**超时与重试**：`skill_call`、`llm` 的 `timeout` 是单次执行超时，默认分别为 30 秒、2 分钟，上限 10 分钟，超时按错误码 `timeout` 失败。节点可以配置 `retry` 覆盖默认重试策略；`non_retryable_errors` 列出不重试的错误码，平台内置 `timeout`、`unknown_skill`，skill 也可以返回自己的错误码。重试耗尽后有 `error` 端口则走它，节点输出为 `{ error, code }`，否则 run 进入人工干预。`human_choice` 的 `timeout` 从节点进入等待算起，卡片在进入等待之后发送。节点配置表单可以用 `wbctl node-types` 或 `CatalogService.ListNodeTypes` 拿到每种节点的独立配置 schema。

## 连线

`{ id, from, port, to }`。普通连线必须构成有向无环图；退回用 `kind: rework`，必须同时设置 `max_iterations` 和 `on_exhausted`，源节点必须是 `human_choice`，目标节点必须位于源节点的每条上游路径上。

## 来源

| 字段 | 说明 |
| --- | --- |
| `kind` | `cron`（平台按 `schedule` 调用查询 `skill` 增量扫描）/ `kafka` / `api`（生产方按标准信封投递 `event_type`） |
| `mapping` | payload 字段 → 表达式（基于 `record`），映射结果按 `schema` 校验 |
| `filter` | 为 true 的记录创建任务；同一 `dedup_key` 的任务已存在时跳过 |
| `close_when` | 为 true 的记录按 `dedup_key` 找到未完结的任务，取消它的 run，任务以 `auto_closed` 完结（run 尚未启动时直接关闭任务）；先于 `filter` 求值，需要配置 `dedup_key` |

`close_when` 让外部状态变化直接推动流程：比如工单在中途审核完成，扫描到状态变化就结束提醒，不必等到下一个定时检查点。

**去重键要能区分“同一件事的不同轮次”**。同一去重键的任务已完结后，再扫描到也不会建新任务。工单被驳回后重新提交属于一次新的审核，所以 VIP 示例的去重键是 `{{ payload.appeal_id }}:{{ payload.commit_time_ms }}`；审核通过等状态变化不改变提交时间，`close_when` 仍能按同一个键找到任务。

**扫描的可靠性**：单条记录映射、校验或表达式求值失败时跳过并计数；数据库等暂时性失败会停止本次扫描、保留游标，下次重扫。内置的 appeal 扫描只读取 `lag_seconds`（默认 60 秒）之前更新的行，覆盖长事务与从库延迟。

## view 与 assignment 的引用和继承

- **view**：`views` 中的名称、共享组件 `key@version`，或 `{ default, variants: [{ when, view }] }`。
- **assignment**：内嵌策略 `{ resolvers, filters, strategy, exclude, fallback }`，或共享组件 `key@version`。
- **继承**：`human_choice` 与 `lark_card` 不写 view / assignment（`lark_card` 中叫 `to`）时，沿用上一个 `human_choice` 的配置，没有则用任务类型默认值。多条上游路径继承到的配置不一致时报 `inherit_conflict`。每个节点最终用的是哪一份，在编译结果的 `bindings` 中可以看到。

## 问题格式

```json
{ "code": "unknown_port", "severity": "error", "path": "workflow.edges.e12.port",
  "node_id": "escalate", "edge_id": "e12", "message": "节点 \"escalate\"（human_choice）没有端口 \"done\"，可用端口：handled" }
```

`path` 用 ID 而不是数组下标定位，前端可以直接高亮对应的节点、连线或字段。`code` 的全部取值见 `internal/flow/compiler` 中的 `IssueCode`。

## 命令行

一期没有前端时，用 YAML 编写任务类型：

```bash
go run ./cmd/wbctl validate examples/task_types/*.yaml   # 校验
go run ./cmd/wbctl json examples/task_types/vip_appeal_alert.yaml   # 输出 body_json
go run ./cmd/wbctl plan examples/task_types/vip_appeal_alert.yaml   # 输出执行计划
go run ./cmd/wbctl node-types                           # 节点类型目录
```

YAML 按 1.2 解析：`yes`、`no`、`on`、`off` 都是字符串，只有 `true` / `false` 是布尔值。
