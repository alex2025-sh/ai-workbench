# 轻量工作流引擎设计

Oct 16, 2026 · @rex

## 目标与边界

自研一个 **DSL 解释型、状态显式持久化** 的轻量工作流引擎，只依赖 TiDB 或 MySQL 8.0（均为 MySQL 协议）和已有的 xxl-job，替代 Temporal。核心判断：我们的 workflow 是图数据而不是代码，运行状态可以直接存表，因此不需要 Temporal 的确定性回放体系。

**要做的能力**

- 图模型 DSL 的编译与执行：分支、并行、汇合、死路径消除。
- 持久化执行：进程崩溃、发版重启后从数据库恢复，不丢步骤。
- 步骤重试、超时、失败后人工干预。
- 持久化定时器：从几十秒到数周的等待。
- 外部信号：人工选择、外部系统回调，带鉴权与校验。
- 受控的环：人工退回上游节点，有次数上限。
- 运行状态可查询，支撑画布着色与审计。

**不做的能力**

- 用代码编写任意 workflow（只执行 DSL）。
- 确定性回放、事件溯源式的执行历史。
- 跨机房多活、毫秒级调度延迟。
- 子流程、动态 fork（后续按需在 v2 之后加入）。

**规模假设**：内部业务流程，量级远低于通用编排平台，步骤调度延迟秒级可接受，定时器精度几十秒可接受。

## 参考项目与借鉴点

不照搬任何一个项目：**执行模型参考 Conductor，调度与租约参考 Camunda 和 River，汇合语义参考 Airflow，交互语义参考 Temporal 和 Dify**。

| 项目 | 借鉴 | 不借鉴 |
| --- | --- | --- |
| [Conductor](https://docs.conductor-oss.org/devguide/how-tos/Tasks/choosing-tasks) | 最接近的参照物：JSON 定义的 workflow，状态存库；系统任务分类（SWITCH、FORK\_JOIN/JOIN、DO\_WHILE、WAIT、HUMAN）；WAIT 等时间、HUMAN 等外部信号的区分；每次执行保存不可变的定义快照；AI 生成的定义与人工设计的定义跑在同一个运行时；循环必须有上限 | Java 服务端与额外存储依赖；worker 通过 HTTP 轮询服务端 |
| [Camunda 7 Job Executor](https://camunda.com/blog/2015/09/scaling-camunda-bpm-in-cluster-job/) | 用锁拥有者 + 锁过期时间认领 job，乐观锁防止并发认领；认领冲突时指数退避；它踩过的坑：执行超过锁时长的 job 会被重复认领，需要防护校验 | BPMN 的完整规范与复杂度 |
| [River](https://riverqueue.com/) | 事务内入队：业务写入与 job 插入同一事务提交；唯一 job；job 完成与业务变更同事务提交 | `SKIP LOCKED` 认领（TiDB 不支持该语法） |
| [DBOS Transact](https://docs.dbos.dev/golang/integrating-dbos) | 每完成一步就把输出检查点写入数据库，崩溃后从最后完成的一步恢复；持久化 sleep | 以代码定义 workflow 的模型 |
| [go-workflows](https://github.com/cschleiden/go-workflows) | Go 风格的执行器接口设计；它缺少 workflow 版本管理，印证了“运行实例锁定定义版本”的必要性 | 基于历史回放的执行模型 |
| [Airflow 触发规则](https://www.astronomer.io/docs/learn/airflow-trigger-rules) | 汇合语义：分支之后的节点用 `none_failed` 或 `one_success` 而非 `all_success`；skipped 状态向下游传播 | 调度器轮询数据库推进 DAG 的整体架构 |
| Temporal | 重试策略字段（初始间隔、退避系数、最大间隔、最大次数）；Update 先校验后接受的信号语义；workflow ID 唯一性 | 确定性回放、事件历史、workflow 代码版本管理 |
| Dify | 画布模型：节点 + 端口 + 连线；Human Input 节点的按钮即端口、超时即分支 | 以 LLM 应用为中心的节点体系 |

## 总体架构

引擎以库的形式运行在业务 Go 进程内，只依赖 TiDB 或 MySQL 和 xxl-job；**推进器是唯一修改节点状态的组件**，其他组件都通过它推进图。

```mermaid
flowchart TB
    Cfg[配置中心<br/>发布 DSL] --> Comp[编译器<br/>校验 · 生成 Plan]
    TaskC[任务中心<br/>启动 · 取消 run] --> API[引擎 API<br/>StartRun · Signal · Cancel · Admin]
    LarkCB[Lark 回调<br/>人工选择信号] --> API
    XXL[xxl-job<br/>wf-ticker 定时触发] --> Tick[Ticker 处理器<br/>到期 job · 租约 · cron]
    API --> Adv[[推进器<br/>锁 run · 防护校验 · 传播 · 写 job]]
    Tick --> Pool[Worker 池<br/>CAS 认领 · 事务外执行]
    Pool <--> Adv
    Pool --> Exec[节点执行器<br/>InvokeAction 分发]
    Exec --> Ext[外部系统<br/>Skill/MCP · LiteLLM · Lark]
    Comp --> DB[(TiDB / MySQL<br/>plan · run · node_run · job · loop_counter · schedule · event)]
    Adv --> DB
```

Worker 池认领 job、在事务外调用节点执行器，把结果交给推进器提交；推进器提交后把新就绪的 job 派发回 Worker 池。引擎 API 的 Signal 与 Cancel 同样经推进器落库。

| 组件 | 职责 |
| --- | --- |
| 编译器 | 发布时校验 DSL，生成不可变的 Plan |
| 引擎 API | `StartRun`（按 `biz_key` 幂等）、`Signal`、`Cancel`、管理操作 |
| 推进器 | 锁 run 行；防护校验；写节点结果；边传播与死路径消除；插入新 job；环的区域重置 |
| Worker 池 | CAS 认领 job，事务外执行节点，提交结果 |
| 节点执行器 | 原生节点本地处理；动作节点统一经 `InvokeAction` 分发到 skill、LLM、Lark、邮件 |
| Ticker 处理器 | 响应 xxl-job：触发到期 job、回收过期租约、触发 cron 来源、巡检告警 |

## 数据模型

8 张表，全部在同一个库（TiDB 或 MySQL）。定时器不单独建表，统一作为带 `due_at` 的 job；边状态不单独存储，由节点状态和端口推导。

```sql
-- 编译后的执行计划，发布后不可变
wf_plan (
  plan_id, version, dsl JSON, compiled JSON, checksum, published_at,
  PRIMARY KEY (plan_id, version)
)

-- 运行实例
wf_run (
  id BIGINT AUTO_RANDOM PRIMARY KEY,
  plan_id, plan_version,            -- 启动时锁定，之后不变
  biz_key UNIQUE,                   -- 业务幂等键，如 task_id，防止重复启动
  status,                           -- running / succeeded / failed / needs_intervention / cancelled
  vars JSON, version INT,           -- version 用于乐观并发与审计
  created_at, updated_at
)

-- 节点的当前状态，每个节点一行
wf_node_run (
  run_id, node_id,
  iteration INT DEFAULT 0,          -- v1 恒为 0，为受控的环预留
  status,                           -- pending / ready / running / waiting / succeeded / skipped / failed
  port,                             -- 节点完成时选中的输出端口
  output JSON,                      -- 大输出只存引用
  attempt INT, wait_until, started_at, finished_at,
  PRIMARY KEY (run_id, node_id)
)

-- 环重置时归档旧轮次，用于审计与画布回放
wf_node_run_history ( run_id, node_id, iteration, status, port, output, ... )

-- 统一的任务表：立即执行、超时、唤醒、定时器都是 job
wf_job (
  id BIGINT AUTO_RANDOM PRIMARY KEY,
  run_id, node_id, iteration,
  kind,                             -- execute / timeout / wake / timer / sync_task
  due_at, status,                   -- pending / done / cancelled
  attempt, owner, locked_until,     -- 租约
  idem_key UNIQUE,                  -- run_id:node_id:iteration:kind
  INDEX (status, due_at)
)

-- 退回边的计数
wf_loop_counter ( run_id, edge_id, count, PRIMARY KEY (run_id, edge_id) )

-- cron 来源，由 ticker 计算并推进 next_fire_at
wf_schedule ( id, cron, timezone, target, next_fire_at, enabled, version )

-- 追加写的事件日志：状态变更、信号、操作人
wf_event ( id, run_id, node_id, iteration, type, payload JSON, actor, created_at )
```

设计要点：

- **`wf_run.biz_key` 唯一**：同一个任务不会因为事件重复投递而启动两个 run。
- **主键与热点**：实际表结构见 `deploy/sql/engine.sql`。为适配 goctl（单列主键、Insert 省略自增列），联合主键改为 `id` 主键 + 联合唯一键，主键用 `AUTO_INCREMENT`，TiDB 上配合 `NONCLUSTERED` 主键与 `SHARD_ROW_ID_BITS` 打散写入；规模上来后再评估改为 `AUTO_RANDOM` 并由应用生成 ID。
- **TiDB 与 MySQL 的差异**：设计只用两者共有的能力（主键 `SELECT ... FOR UPDATE`、CAS 更新、JSON 列），MySQL 需 8.0 及以上。需要注意三点：TiDB 多节点下自增 `id` 不保证单调，排序和去重一律用 `seq` 或业务唯一键；TiDB 有单事务大小上限，推进事务只写一个 run 的少量行，不受影响；TiDB 专用选项写在 `/*T! */` 注释中，MySQL 会忽略，同一份 DDL 两边都能执行。
- **`wf_plan` 的版本从属于任务类型**：`plan_id` 为任务类型 key，`version` 即任务类型的发布版本，由发布清单引用；workflow 不单独对用户暴露版本号（见《AI 任务平台设计概要》「版本管理」）。
- **`wf_event` 只追加、不更新**：审计、画布回放、指标统计都从这里读，与执行状态表解耦。

## DSL 与编译器

DSL 是“节点 + 端口 + 连线”的图；编译器在发布时完成全部结构校验，产出不可变的执行计划，运行时不再做结构判断。

### 节点类型

| 节点 | 类别 | 输出端口 | 说明 |
| --- | --- | --- | --- |
| `trigger` | 原生 | `next` | 入口，接收触发数据 |
| `condition` | 原生 | 每个分支一个端口 + `default` | 表达式求值，选中一个端口 |
| `merge` | 原生 | `next` | 汇合，模式 `all` / `any` |
| `timer` | 原生 | `next` | 等待固定时长或到某个时间点 |
| `human_choice` | 原生 | 每个选项一个端口；配置超时时另有 `timeout` | 发卡片并等待人工选择，可配置重复提醒 |
| `skill_call` / `llm` / `lark_card` / `send_email` / `emit_event` | 动作 | `next`、`error` | 统一经 `InvokeAction` 执行外部 IO |
| `end` | 原生 | 无 | 结束 |

所有节点都可以挂 `side_effects`（旁路动作），以 `async` 方式执行，不阻塞主路径。

### 汇合语义

借鉴 Airflow 的触发规则，`merge` 只提供两种模式：

- `all`：所有入边都有结论，且至少一条触发（相当于 `none_failed_min_one_success`）。用于并行分支汇合，以及条件分支之后的汇合。
- `any`：任一入边触发即执行，之后到达的忽略（相当于 `one_success`）。

普通节点有多条入边时默认按 `all` 处理。

### 编译期校验

1. **结构**：去掉 `rework` 边后拓扑排序必须成功；所有节点从 `trigger` 可达；除 `end` 外每个节点都有出边。
2. **端口**：每条边的源端口必须是该节点类型声明的端口；`human_choice` 配置了超时则 `timeout` 端口必须有连线，未配置则不允许连线。
3. **配置**：每个节点的配置按其类型的 JSON Schema 校验；引用的 skill、模板、任务类型必须存在且已发布。
4. **变量引用**：`{{nodes.X.output.*}}` 中的 X 必须位于当前节点的上游且有路径可达；表达式在编译期预编译，语法错误直接报出。
5. **受控的环**：见“受控的环”一节。

### 执行计划（Plan）

编译产物包括：按拓扑序排好的节点列表、每个节点的入边与出边（切片，顺序固定）、每条 `rework` 边对应的环区域节点集合、预编译的表达式。运行时只读 Plan，不再遍历原始 DSL。

## 执行模型

执行分为两段：**事务外执行节点（外部 IO），事务内推进图**。同一个 run 的所有推进通过锁 `wf_run` 行串行化，这是正确性的核心。

### 一次节点执行的完整流程

1. **认领**：worker 用 CAS 抢占 job，参考 Camunda 的锁拥有者 + 锁过期时间做法。TiDB 不支持 `SKIP LOCKED`，CAS 方式在 TiDB 上可直接使用。MySQL 8.0 虽然支持 SKIP LOCKED，也统一用 CAS，两种库的行为保持一致。
2. **执行**：事务外调用节点执行器（skill、LLM、发卡片等），不持有任何数据库锁。
3. **提交**：开启悲观事务，锁 run 行，做防护校验，写结果，传播，插入新 job，全部一次提交。
4. **派发**：提交成功后，把新就绪的 job 直接交给本进程 worker 池；ticker 只做兜底。

```sql
-- 1. 认领（影响行数为 0 = 被别人抢走）
UPDATE wf_job
SET owner = ?, locked_until = NOW() + INTERVAL ? SECOND, attempt = attempt + 1
WHERE id = ? AND status = 'pending' AND due_at <= NOW()
  AND (owner IS NULL OR locked_until < NOW());
```

```go
// 3. 提交（伪代码）
tx := db.BeginPessimistic()
run := tx.SelectForUpdate("wf_run", runID)          // 串行化同一 run 的推进
node := tx.Get("wf_node_run", runID, nodeID)

// 防护校验：节点仍在执行、轮次和尝试次数与认领时一致
if node.Status != Running || node.Iteration != job.Iteration || node.Attempt != job.Attempt {
    tx.Rollback()                                      // 僵尸 worker 或已被退回，结果丢弃
    return
}

tx.CompleteNode(node, result.Port, result.Output)
ready := propagate(tx, plan, run, nodeID, result.Port) // 死路径消除，返回新就绪节点
for _, n := range ready {
    tx.InsertJob(run.ID, n, "execute", now)            // idem_key 唯一，重复插入直接失败
}
if noRunningOrWaiting(tx, run) {
    tx.FinishRun(run)
}
tx.AppendEvents(...)
tx.Commit()
```

### 边传播与死路径消除

边的状态由源节点推导：源节点 `succeeded` 且端口匹配为触发，源节点 `skipped` 或走了其他端口为跳过。传播逻辑：

- 对目标节点检查入边：全部有结论且至少一条触发 → 就绪；全部跳过 → 标记 `skipped` 并继续向下传播；还有未决入边 → 等待。
- 从 `pending` 到 `ready` 的迁移带条件更新（`WHERE status = 'pending'`），保证每个节点在一轮中只被调度一次。
- 遍历 Plan 中预先排好序的切片，不遍历 map，结果稳定、可复现。

### 为什么需要防护校验

Camunda 记录过一个典型问题：执行时间超过锁时长的 job 会被另一个执行器重新认领，前一个执行器完成后再写回，就会产生冲突甚至并行执行。我们用 `attempt` 和 `iteration` 做围栏：重新认领时 `attempt` 加 1，旧 worker 提交时发现不一致，结果直接丢弃。外部副作用依靠幂等键兜底（见“失败处理、幂等与恢复”）。

## 人工节点、定时器与 xxl-job 时钟

所有“等待”都落成带 `due_at` 的 job；xxl-job 只负责按时唤醒，状态与逻辑全部在引擎内。

### human\_choice 节点

```yaml
- id: ask_rm
  type: human_choice
  assignment: vip_rm_policy@1        # 可省略：继承上一个人工节点（见“人工节点的处理人与展示”）
  view: vip_ticket_card
  options: [contacted, snooze, dismiss]
  form: { dismiss: [reason] }        # 某些选项要求必填
  timeout: 48h                        # 可选；也可以是表达式；不配置则一直等待
  remind: { every: 24h, max: 3 }      # 可选；等待期间重复提醒
```

| 事件 | 引擎动作（均在锁 run 行的事务内） |
| --- | --- |
| 节点执行 | 发卡片；节点置为 `waiting`；配置了超时则插入 `timeout` job，配置了提醒则插入 `wake` job |
| RM 点击 | API 先按任务表鉴权；事务内确认节点仍 `waiting`、轮次一致、选项合法、必填项齐全；写端口；作废该节点的 timeout / wake job；传播 |
| 选择 snooze | 节点保持 `waiting`，作废原 wake job，按 snooze 时长插入新的 wake job，并同步任务状态为 snoozed |
| wake 到期 | 重新发卡片（幂等键带提醒序号），按配置插入下一次 wake；若来自推迟，同步任务回迁 pending |
| timeout 到期 | 若节点仍 `waiting`，按 `timeout` 端口完成并传播 |

点击与超时在同一行锁下竞争，只有先到的生效。信号语义参考 Temporal 的 Update：先校验，不通过直接拒绝并返回原因，卡片上即时提示。

### timer 节点

执行时节点置为 `waiting`，插入 `timer` job；到期后以 `next` 端口完成。

### xxl-job ticker

只注册一个调度任务 `wf-ticker`，每 10 到 30 秒触发一次：

1. 认领并执行到期的 `timeout` / `wake` / `timer` job。
2. 回收租约过期的 `execute` job，使其可被重新认领。
3. 扫描 `wf_schedule`，对到期的 cron 来源触发扫描或启动 run，然后用乐观锁推进 `next_fire_at`，防止多实例重复触发。
4. 巡检：长时间 `waiting` 或 `needs_intervention` 的 run 告警。

要求：ticker 幂等，重复触发无副作用，漏触发由下一次补上。实例多了之后使用 xxl-job 的分片广播，按 `run_id` 取模分担扫描。

不把每个 cron 来源注册为独立的 xxl-job 任务：AI 生成的定时来源会持续增加，集中在 `wf_schedule` 表里管理，配置只存在一处，xxl-job 侧无需维护。

## 受控的环

只允许显式声明、由人工决策触发、有次数上限、单入口的环；普通连线永远不能成环。Conductor 的 DO\_WHILE 同样要求循环有边界，这是同一个原则。

### DSL

```yaml
- from: review
  port: return
  to: supplement
  kind: rework            # 显式标记
  max_iterations: 3       # 必填
  on_exhausted: escalate  # 必填：超过次数后的去向
```

### 编译期校验

1. 去掉 `rework` 边后必须是 DAG。
2. `rework` 边的源节点必须是 `human_choice`；若用 `condition` 驱动自动循环，必须同时配置最大次数和每轮最小间隔。
3. 单入口：目标节点必须支配源节点（从入口到源节点的所有路径都经过目标节点）；环区域内的节点只能从目标节点进入。
4. 多个环之间互不相交或完全嵌套。
5. `on_exhausted` 指向的节点存在且不在该环区域内。

### 运行期：退回 = 区域重置

在锁 run 行的事务内：

1. `wf_loop_counter` 加 1；超过 `max_iterations` 则改走 `on_exhausted`。
2. 环区域内所有节点的当前记录归档到 `wf_node_run_history`，重置为 `pending`，`iteration + 1`。
3. 目标节点直接置为 `ready`（它来自环外的入边早已有结论）。
4. 环区域内仍在执行的 job、未触发的 timeout / wake job 不需要逐个取消：它们携带旧的 `iteration`，提交或触发时防护校验失败，自动作废。

下一轮可以通过 `{{loop.iteration}}`、`{{nodes.review.prev.output.reason}}` 读取上一轮的退回原因，卡片上展示给补充信息的人。

### 防死循环

| 层次 | 机制 |
| --- | --- |
| 编译期 | 普通边不成环；回边显式声明；源节点限定；单入口结构 |
| 单个环 | `max_iterations` 上限，超出走 `on_exhausted`（通常是升级） |
| 自动循环 | 每轮最小间隔 |
| 整个 run | 节点总执行次数与运行总时长上限，超出进入 `needs_intervention` 并告警 |
| 运营 | 统计退回频繁的流程与节点 |

## 与任务中心的集成

**引擎是流程的事实来源，任务表是它的投影**。两者不在同一个事务里提交，靠引擎内的事务外发 job 保证最终一致，这与 Temporal 用 Activity 更新业务库、靠重试和幂等收敛的做法一致。

### 状态同步

1. 推进器在推进事务内插入一个 `kind = sync_task` 的 job，携带 `task_id`、目标状态、当前处理人和 `seq`（取 `wf_run.version`）。它与节点状态的变更同事务提交，二者要么都发生，要么都不发生。
2. worker 执行该 job，调用任务中心 `UpdateStatus(task_id, status, assignee, seq)`。任务中心只接受比当前 `seq` 更新的写入，重复或乱序到达的请求直接忽略。
3. 调用失败按重试策略重试，任务状态通常在秒级内与引擎一致。

xxl-job 只通知一台机器，解决的是 ticker 不重复触发；这里防的是**部分失败**（节点状态写入后进程崩溃、任务状态没来得及更新），由带重试的 `sync_task` job 补齐。任务中心将来独立部署、独立数据库也不受影响。

### 写入入口

| 操作 | 入口 | 说明 |
| --- | --- | --- |
| 选择选项、推迟、退回 | 引擎 Signal API | 影响流程；引擎推进后再同步任务状态 |
| 开始处理（pending → processing） | 任务中心 | 不影响流程，引擎不感知 |
| 认领组任务（unassigned → pending） | 任务中心（分配服务 CAS） | 只改变处理人；Signal 鉴权读取任务表，自然生效 |
| 调整优先级 | 任务中心 `UpdatePriority` | 不影响流程 |
| 源工单离开 Maker Review Case | 任务中心 → 引擎 `Cancel` | 任务置为 solved（auto\_closed），run 取消 |

### 节点事件到任务状态的映射

| 引擎事件 | 任务状态（取值对齐 Morning Brief） |
| --- | --- |
| 进入人工节点，处理人已确定 | `pending`（0），assignee 更新为该节点处理人 |
| 进入人工节点，处理人待认领 | `unassigned`（-1） |
| 处理人选择推迟 | `snoozed`（2），写入 `snooze_until` |
| wake job 到期 | 回迁 `pending`（0） |
| 选择后进入下一个人工节点 | 仍为 `pending`，assignee 切换为下一节点处理人 |
| run 正常结束 | `solved`（3），resolution 取自最后一个人工节点的选项或 DSL 配置 |
| run 被取消 | `solved`（3），resolution 为 auto\_closed |

## 人工节点的处理人与展示

**每个人工节点只由一个人决策**；需要多人参与时拆成多个人工节点串联。指派给用户组时，组内第一个认领的人成为处理人，仍是单人决策。因此引擎不需要会签、投票这类多人决策状态。

### 节点执行顺序

`human_choice` 节点执行时依次做三件事，前两步都是动作，经 `InvokeAction` 调用：

1. **确定处理人**：调用分配服务，按该节点的分配策略求值（策略链、跨部门协议、职责分离排除），结果写入任务表。
2. **发送卡片**：按该节点的展示模板与受众变体渲染并发送；记录 `task_id → message_id`，用于后续更新卡片。
3. **进入等待**：节点置为 `waiting`，按配置插入 timeout / wake job，同时插入 `sync_task` job。

### 编译期继承

节点不写 `view` 或 `assignment` 时，沿用上一个人工节点的配置；路径上没有更早的人工节点时，回退到任务类型默认值。编译器沿拓扑序计算并写入 Plan：

- 汇合后不同路径上的“上一个人工节点”配置不一致 → 编译报错，要求显式配置。
- 继承的是策略，不是人：运行时重新求值；需要固定为上一节点的实际处理人时，使用 `previous_assignee` 解析器。
- 环内节点的继承同样在编译期确定，每一轮使用相同配置。

### AI 摘要

模板开启 `ai_summary` 时，摘要作为节点的 `async` 旁路动作执行：卡片先发出，摘要生成后更新原卡片；失败（超时、报错、为空、超长）则不显示，只记入事件与指标。摘要只使用当前受众可见、已脱敏的字段，workflow 的判断逻辑从不读取摘要内容。

## 试运行（dry-run）

配置发布前必须试运行，AI 生成的 workflow 也靠它评估。试运行复用同一个编译器和推进器，只替换会产生副作用的部分，因此试出来的路径就是上线后的真实路径。

| 部分 | 真实运行 | 试运行 |
| --- | --- | --- |
| 编译器、推进器、边传播 | 相同 | 相同 |
| 查询类 skill | 调用 | 调用（只读，走只读副本） |
| 动作类节点（发卡片、邮件、通知外部系统） | 调用 | 模拟执行器：记录“发给谁、发什么”，不真正调用 |
| 分配服务 | 调用并写任务表 | 调用求值，结果只写入报告 |
| 人工节点 | 等待真实选择 | 按预设的选择或场景脚本自动推进 |
| 时间 | 真实时钟 | 虚拟时钟，超时与提醒即时快进 |
| 存储 | 正式表 | 内存或带 `dry_run` 标记的隔离数据，不触发 ticker |

**输入**：一组历史事件样本（如过去 7 天的工单），或手工构造的 payload。

**输出报告**：每个样本走过的路径与端口、每个人工节点的处理人和渲染后的卡片、模拟发送的消息清单、超时与升级的触发时间点，以及编译告警。评审人看报告批准发布，AI 生成配置时也用同一份报告做自动检查。

## 失败处理、幂等与恢复

引擎保证“步骤至少执行一次、图的推进恰好一次”；外部副作用的“恰好一次”由幂等键实现。

### 重试

重试策略字段参考 Temporal：初始间隔、退避系数、最大间隔、最大次数、不可重试的错误类型。每种节点类型有默认值，DSL 只能在平台允许的范围内覆盖。重试 = 把 job 的 `due_at` 设为退避后的时间，`attempt` 递增。

重试耗尽后：节点有 `error` 端口则走 `error` 端口；否则 run 进入 `needs_intervention`，运维可选择重试该节点、跳过（指定端口）或终止 run。所有人工干预写入 `wf_event`。

### 幂等

| 层次 | 幂等键 | 防止的问题 |
| --- | --- | --- |
| run 启动 | `wf_run.biz_key` | 事件重复投递导致重复启动 |
| job 插入 | `run_id:node_id:iteration:kind` | 传播重复执行导致重复调度 |
| 外部动作 | `run_id:node_id:iteration[:序号]`，随请求传给下游 | worker 在外部调用后、提交前崩溃导致的重复发送 |
| 信号 | 节点状态条件更新 | 重复点击、点击与超时并发 |

事务内入队参考 River：节点完成、传播结果、新 job 插入在同一事务提交，不存在“状态已更新但 job 没插入”的中间态，也不需要额外的 outbox。

### 恢复

| 故障 | 恢复方式 |
| --- | --- |
| worker 执行中崩溃 | 租约过期后 ticker 回收，job 重新认领，`attempt + 1` |
| 提交事务失败 | 事务回滚，job 仍为认领状态，租约过期后重试 |
| 进程发版重启 | 状态全部在库中，重启后由 ticker 与 worker 继续 |
| xxl-job 漏触发 | 下一次 tick 补上，延迟一个周期 |
| 数据库短暂不可用 | worker 认领失败指数退避（参考 Camunda 的认领退避） |

## 可观测、运维与测试

状态都在表里，可观测性几乎是免费的；测试的重点是并发与故障，而不是单条路径。

### 可观测

- **画布着色**：运行态画布直接查询 `wf_node_run`（当前轮）与 `wf_node_run_history`（历史轮），显示每个节点的状态、端口、轮次。
- **指标**：job 等待时长（`NOW() - due_at`）、认领冲突率、节点失败率、`needs_intervention` 数量、人工节点等待时长分布。
- **追踪**：每个 job 带 `run_id`、`node_id`、`iteration` 作为日志与 trace 的公共字段。

### 运维操作

提供管理 API 与控制台：查看 run、重试节点、跳过节点（指定端口）、终止 run、手动触发 wake。每个操作需要权限并写入 `wf_event`。

### 数据保留

`wf_event` 与 `wf_node_run_history` 按月分区，超过保留期归档到对象存储；已结束的 `wf_job` 定期清理。

### 测试策略

| 类型 | 覆盖内容 |
| --- | --- |
| 编译器单测 | 环检测、端口校验、支配关系与单入口判定、变量引用可达性 |
| 传播单测 | 表驱动：分支、汇合 all/any、跳过向下传播、嵌套分支 |
| 时间相关 | 注入可控时钟，快进验证 48h 超时、重复提醒、snooze |
| 并发 | 两个分支同时到达 `merge`；点击与超时同时发生；同一 job 被两个 worker 认领 |
| 故障注入 | 在认领后、外部调用后、提交前、提交后分别杀掉 worker，验证无丢失、无重复推进 |
| 退回 | 区域重置后旧轮次的 job 全部作废；达到上限后走 `on_exhausted` |

## 扩展性

引擎的执行模型核心参考 Conductor 的“决策器 + 持久化任务状态”：定义是数据，状态存库；每个任务完成后由决策器对照定义和当前状态算出下一步，后台扫描定期修复卡住的流程。我们的推进器对应决策器，ticker 对应后台扫描。判断扩展成本的标准是：**新需求能否由下面四个原语组合出来**。

| 原语 | 含义 |
| --- | --- |
| 完成节点 | 以某个端口结束节点并传播 |
| 带时间的等待 | 节点进入 `waiting`，插入带 `due_at` 的 job |
| 信号 | 外部输入经校验后完成等待中的节点 |
| 区域重置 | 退回时重置环内节点，`iteration + 1` |

### 扩展方向

| 方向 | 核心改动 | 例子 |
| --- | --- | --- |
| 新增动作 | 无，在 `InvokeAction` 注册实现 | 新的 skill、短信、写入工单系统 |
| 新增节点类型 | 无，组合现有原语 | 等待外部事件（信号 + 按业务键关联 Kafka 事件）、Agent 节点（动作，内部循环由 agent 管理）、审批节点 |
| 新增触发来源 | 无 | Kafka 事件、新的 cron 来源 |
| 子流程、动态扇出 | 新增一个原语：启动子 run，完成时回调父节点 | 复用合规复核流程；对一组 VIP 逐个跟进 |
| 调度与限流 | 小：`wf_job` 加优先级字段，按动作类型配置并发与速率 | P0 任务优先；Lark API 调用频率限制 |
| 在途实例迁移 | 小：兼容性检查 + 管理 API | 修复流程缺陷后让在途 run 切到新版本 |
| 规模 | 不改模型 | 按 `run_id` 分片 worker 与 ticker |
| 执行后端 | 不改 DSL 与业务 | 通过执行器接口切换到 Temporal |

### 不擅长的场景

- **用代码表达的复杂长流程**（大量计算、条件、循环）：写成 Go 的系统节点，在 DSL 中整体调用，不扩展 DSL 的表达能力。
- **极高吞吐**：每次推进都锁 run 行、写 job 表，适合内部业务流程，不适合海量实例的通用编排平台。

## 分阶段实现

先把并发正确性做扎实，再加能力；每一阶段都以 VIP 工单提醒作为验收用例。

1. **阶段一：核心执行**。数据模型（含 `iteration` 字段，恒为 0）；编译器（拓扑排序、端口与配置校验）；认领、执行、提交、传播；`condition`、`merge`、动作节点；并发与故障注入测试。验收：手写 DSL 跑通无人工节点的流程。
2. **阶段二：等待与时钟**。`human_choice`（超时可选、重复提醒、snooze）、`timer`；xxl-job ticker；租约回收；`wf_schedule` cron 来源；Lark 回调接入信号 API；任务状态同步（sync\_task）；节点级分配、模板与编译期继承。验收：VIP 工单提醒端到端上线。
3. **阶段三：运维与可视化**。管理 API 与人工干预；运行态画布着色；试运行；指标与告警；数据保留。
4. **阶段四：受控的环**。`rework` 边的编译期校验（支配关系、单入口、嵌套）；区域重置；防死循环上限。

执行器接口从阶段一起就保持精简（执行节点、设置定时、等待信号、查询状态），以后如需接入 Temporal，可作为另一种实现替换，DSL 与上层业务不受影响。

## Sources

- [Conductor OSS: Choosing Tasks](https://docs.conductor-oss.org/devguide/how-tos/Tasks/choosing-tasks)
- [Orkes Conductor: System Tasks](https://orkes.io/content/documentation/configuration/workflowdef/systemtasks)
- [Conductor OSS: Durable Agents](https://docs.conductor-oss.org/devguide/ai/durable-agents)
- [Camunda: Scaling Camunda BPM in a Cluster — Job Executors and Coordination](https://camunda.com/blog/2015/09/scaling-camunda-bpm-in-cluster-job/)
- [Camunda Forum: Jobs are not being executed (lock expiration)](https://forum.camunda.io/t/jobs-are-not-being-executed-or-with-heavy-delay/469)
- [River](https://riverqueue.com/)
- [DBOS Transact Go: Add DBOS To Your App](https://docs.dbos.dev/golang/integrating-dbos)
- [go-workflows](https://github.com/cschleiden/go-workflows)
- [Astronomer: Airflow trigger rules](https://www.astronomer.io/docs/learn/airflow-trigger-rules)
- [TiDB: MySQL compatibility](https://docs.pingcap.com/tidbcloud/mysql-compatibility)
- [Ask TiDB: SELECT FOR UPDATE](https://ask.pingcap.com/t/does-tidb-support-select-for-update/163)
