# 任务类型 JSON 说明

任务类型 = **数据源 + 业务字段 + 人工节点链 + 卡片**。配置页的四步表单、仓库中的 YAML（`examples/task_types/`）最终都是同一份 JSON，作为 `SaveTaskTypeDraft.body_json` 提交。字段的精确约束以 [`api/schema/task_type.schema.json`](../api/schema/task_type.schema.json) 为准。

## 校验

| 层次 | 检查什么 | 在哪里 |
| --- | --- | --- |
| 结构 | 字段名、类型、必填项、取值范围 | `task_type.schema.json`（报错已翻译成中文） |
| 语义 | 字段名唯一、模板占位引用的字段存在、节点 id 唯一、最后一个节点不能升级、link 配置完整、cron 与 SQL 合法 | `internal/tasktype.Check` |
| 引用 | 数据源连接、业务关系是否存在 | `internal/registry.Validate` |

保存草稿时返回全部问题但不阻止保存；发布时有任何问题都会拒绝。问题的 `path`（如 `nodes.1.assignee.relation`）用于在配置页定位到步骤与表单项。

## 顶层结构

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `key` | 是 | 任务类型标识，`^[a-z][a-z0-9_]{0,63}$`，创建后不可修改 |
| `namespace` | | 所属业务域 |
| `name` | 是 | 显示名称 |
| `description` | | 任务说明 |
| `priority` | | 新任务的优先级 `P0`–`P3`，默认 `P2` |
| `source` | 是 | 数据源 |
| `fields` | 是 | 业务字段（有序数组） |
| `card` | 是 | 卡片 |
| `nodes` | 是 | 人工节点链，1–10 个 |

模板（卡片标题、链接地址）只支持字段占位 `{字段名}`，`{key}` 是数据源给出的 key；没有表达式语言，计算写在 SQL 里。

## 数据源 `source`

三种来源归一成同一个语义：**某个 key 当前在 / 不在待办集合里**。在 → 没有跟踪中的任务就创建，有则刷新字段（不重发通知）；不在 → 未完结的任务自动关闭（resolution `auto_closed`）。

处理人手工完结（close / no_response）的任务仍「跟踪中」：key 还在结果里时不会重建，等它从结果中消失一次、再出现才是新任务。同一任务类型、同一 key 只有一个跟踪中的任务（唯一索引 `(task_type, track_key)`）。

| `kind` | 字段 | 说明 |
| --- | --- | --- |
| `sql` | `datasource`、`schedule`、`timezone`、`query` | 平台按 cron 执行查询，**结果集就是当前待办**。`datasource` 是平台注册的只读连接名；`query` 只能是单条 SELECT，必须有一列叫 `key`，其余列就是业务字段。执行：只读事务、超时 10 秒、最多 5000 行；超过 5000 行时只创建不关闭（无法判断哪些 key 消失了） |
| `api` | — | 业务方调用 `IngestService.Push` |
| `kafka` | — | 业务方发到平台固定的 topic `wb.task.events` |

推送的消息：

```json
{ "task_type": "kyc_review", "key": "K1", "op": "upsert", "occurred_at": 1760918400000,
  "fields": { "uid": "10086", "amount": "12.50" } }
```

`op = upsert` 等于「在」，`op = close` 等于「不在」；`occurred_at` 比该 key 已应用的消息旧时忽略（乱序、重复投递）；`fields` 中不认识的字段报错。

时间类规则（如提交 48 小时后提醒、7 天后升级）属于业务数据：写成时间窗口互不重叠的 SQL，拆成多个任务类型，见 `examples/task_types/vip_appeal_*.yaml`。

## 字段 `fields`

| 字段 | 说明 |
| --- | --- |
| `name` | 字段名，SQL 来源为列名；不能叫 `key` |
| `label` | 显示名，用作卡片与详情的标签；缺省显示字段名 |
| `type` | `text`（默认）/ `number` / `money`（字符串承载，不经过 float64）/ `datetime`（Unix 秒、毫秒或日期文本，按 Asia/Shanghai 显示）/ `enum` |
| `pii` | `mask_middle` / `hash` / `drop`：工作台、任务详情与 Lark 都按此显示 |
| `options` | `enum` 的取值：`[{ "value": "COMMITED", "label": "待审核" }]` |

## 卡片 `card`

| 字段 | 说明 |
| --- | --- |
| `title` | 标题模板 |
| `fields` | 卡片上的字段名，顺序即显示顺序；工作台卡片显示前 3 个 |

一个任务类型只有一张卡片，渲染时自动带上当前节点名。工作台、任务详情、配置页预览与 Lark 用同一份渲染结果（`tasktype.RenderCard`）。

## 节点 `nodes`

任务创建时进入第一个节点；`escalate` 进入下一个节点；`close` / `no_response` 结束任务。一个节点只由一个人决策。

| 字段 | 说明 |
| --- | --- |
| `id` | 节点 id，任务记录当前所在的节点。配置页新建节点时自动生成（`n_` + 6 位随机字符）且不展示，改名、调整顺序都不影响在途任务；手写 YAML 时可以用可读的 id |
| `name` | 节点名，显示在卡片与流程图上 |
| `assignee` | 分配器，见下 |
| `actions` | 处理人可用的动作，见下 |
| `link` | 启用 `link` 时必填：`{ "label": "打开工单", "url": "https://.../{appeal_id}" }` |
| `snooze` | 可选的推迟时长，如 `["1h", "4h", "1d"]`（缺省即这三个） |
| `lark` | 分配到人、转交、推迟到期时私聊处理人；认领节点进入时总是发到群，不受此项控制 |

分配器：

| `kind` | 字段 | 进入节点时 |
| --- | --- | --- |
| `relation` | `relation`、`field`、`fallback` | 按业务关系（如 `vip_rm`）用字段值查到一个人；查不到时交给兜底人 `fallback` |
| `pool` | `users`、`strategy`（缺省 `random`） | 从候选人 `users` 中随机选一个 |
| `claim` | `chat`、`remind_after`（可选） | 不指定人，任务进入「待认领」，卡片发到 Lark 群 `chat`；任何人打开链接都可以认领，先到先得。超过 `remind_after`（如 `2h`）仍无人认领时再提醒一次群，不自动升级 |

分配器在 `internal/assign` 中是可注册的 `Resolver`；业务关系是 `Relations`，选择策略是 `Picker`（以后的轮询、按负载都在这里加）。

动作（平台内置，代码里是一个注册表，新增动作不改配置格式）：

| 动作 | 效果 | 任务是否完结 |
| --- | --- | --- |
| `close` | 已处理，resolution `handled` | 是 |
| `no_response` | 无需处理，resolution `no_response` | 是 |
| `snooze` | 推迟：状态 snoozed，到期回到待处理并重新通知 | 否 |
| `link` | 打开业务系统链接，只在前端打开，不改变状态 | 否 |
| `escalate` | 交给下一个节点，重新分配并通知；最后一个节点不能启用 | 否 |
| `transfer` | 转交：留在当前节点，交给另一个人；认领节点还可以放回待认领（重新通知群）。必须写原因，原处理人成为经手人 | 否 |

此外始终可用、不需要配置的任务操作：认领（认领节点上的待认领任务）、开始处理、调整优先级。`task.admin` 在任何节点都可以转交（包括把待认领的任务直接交给某人）。

## 命令行

```bash
go run ./cmd/wbctl validate examples/task_types/*.yaml   # 结构与语义校验
go run ./cmd/wbctl json examples/task_types/vip_appeal_48h.yaml   # 输出 JSON
go run ./cmd/wbctl publish -f app/worker/etc/worker.yaml examples/task_types/*.yaml   # 本地直接发布
```
