// Package engine 是轻量工作流引擎的执行核心：推进器、认领与租约、边传播、任务状态同步。
//
// 核心思想：workflow 是图数据而不是代码，运行状态直接存表，不需要确定性回放。
// 正确性的核心是“事务外执行节点，事务内推进图”：流程状态与 job 状态（含认领）只经 update
// 在锁住 wf_run 行的事务内修改，同一个 run 的一切修改由这把锁串行化。
// 设计见 docs/workflow-engine.md。
package engine

import (
	"encoding/json"
	"strconv"
	"time"
)

// RunStatus 是运行实例的状态。
type RunStatus string

const (
	RunRunning           RunStatus = "running"
	RunSucceeded         RunStatus = "succeeded"
	RunFailed            RunStatus = "failed"
	RunNeedsIntervention RunStatus = "needs_intervention"
	RunCancelled         RunStatus = "cancelled"
)

// Terminal 报告 run 是否已经结束。
func (s RunStatus) Terminal() bool {
	return s == RunSucceeded || s == RunFailed || s == RunCancelled
}

// NodeStatus 是节点在当前轮的状态。
type NodeStatus string

const (
	NodePending   NodeStatus = "pending"
	NodeReady     NodeStatus = "ready"
	NodeRunning   NodeStatus = "running"
	NodeWaiting   NodeStatus = "waiting"
	NodeSucceeded NodeStatus = "succeeded"
	NodeSkipped   NodeStatus = "skipped"
	NodeFailed    NodeStatus = "failed"
)

// Concluded 报告节点在本轮是否已有结论（成功或跳过）。
func (s NodeStatus) Concluded() bool { return s == NodeSucceeded || s == NodeSkipped }

// JobKind 是 job 的类型。所有“现在不能做或要在提交之后做”的工作都落成带 due_at 的 job。
type JobKind string

const (
	JobExecute JobKind = "execute" // 执行节点
	JobTimer   JobKind = "timer"   // timer 节点到期，走 next
	JobTimeout JobKind = "timeout" // human_choice 超时，走 timeout
	JobEffect  JobKind = "effect"  // 节点状态提交之后才执行的外部动作：人工节点的卡片、任务状态同步
)

// graphJobs 是推进图的 job；run 结束或取消时撤销它们。effect 不撤销：已提交的状态仍要触达外部。
var graphJobs = []JobKind{JobExecute, JobTimer, JobTimeout}

// EffectPayload 是 effect job 的参数。
type EffectPayload struct {
	Kind         string          `json:"kind"`
	Config       json.RawMessage `json:"config,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	IdemKey      string          `json:"idem_key"`
	DryRun       bool            `json:"dry_run,omitempty"`
	WhileWaiting bool            `json:"while_waiting,omitempty"` // 只在节点仍在等待时执行（如人工节点的卡片）
}

// JobStatus 是 job 的状态。
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobDone      JobStatus = "done"
	JobCancelled JobStatus = "cancelled"
	JobFailed    JobStatus = "failed" // effect 用尽重试，或毒丸 job 被隔离
)

// Vars 是 run 的变量：触发时的任务 payload 与任务引用。
type Vars struct {
	Payload json.RawMessage `json:"payload"`
	Task    TaskRef         `json:"task"`
}

// TaskRef 是驱动 run 的任务。ID 为 0 表示没有任务（如试运行、测试），不做任务同步。
type TaskRef struct {
	ID       int64  `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Priority string `json:"priority,omitempty"`
	Assignee string `json:"assignee,omitempty"` // 当前处理人，人工节点或通知节点确定后更新
	Group    string `json:"group,omitempty"`    // 待认领的组
}

// Run 是运行实例的领域视图。
type Run struct {
	ID          int64     `json:"id"`
	PlanKey     string    `json:"plan_key"` // 任务类型 key
	PlanVersion int       `json:"plan_version"`
	BizKey      string    `json:"biz_key"`
	Status      RunStatus `json:"status"`
	Vars        Vars      `json:"vars"`
	Version     int64     `json:"version"` // 每次改变状态的推进 +1，同时作为任务同步的 seq
	ExecCount   int       `json:"exec_count"`
	DryRun      bool      `json:"dry_run"`
}

// NodeRun 是节点在当前轮的状态。
type NodeRun struct {
	RunID      int64           `json:"run_id"`
	NodeID     string          `json:"node_id"`
	Iteration  int             `json:"iteration"`
	Status     NodeStatus      `json:"status"`
	Port       string          `json:"port,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Attempt    int             `json:"attempt"`
	WaitUntil  *time.Time      `json:"wait_until,omitempty"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

// Job 是一条待处理的 job 的引用：调度只用它找到 job，状态以 run 锁内读到的行为准。
// Attempt 在认领成功后是这次认领的凭证。
type Job struct {
	ID        int64     `json:"id"`
	RunID     int64     `json:"run_id"`
	NodeID    string    `json:"node_id"`
	Iteration int       `json:"iteration"`
	Kind      JobKind   `json:"kind"`
	DueAt     time.Time `json:"due_at"`
	Attempt   int       `json:"attempt"`
}

// IdemKey 生成 job 的幂等键：run_id:node_id:iteration:kind。
// 带上 iteration，退回后新一轮的同名动作不会被上一轮的幂等记录误拦。
func IdemKey(runID int64, nodeID string, iteration int, kind JobKind) string {
	return strconv.FormatInt(runID, 10) + ":" + nodeID + ":" + strconv.Itoa(iteration) + ":" + string(kind)
}

// SyncPayload 是任务状态同步（kind 为 sync_task 的 effect）的参数，交给任务中心按 seq 幂等应用。
type SyncPayload struct {
	TaskID      int64      `json:"task_id"`
	Status      string     `json:"status"`             // unassigned / pending / snoozed / solved
	Reassign    bool       `json:"reassign,omitempty"` // 本次推进明确分配了处理人；否则任务中心保留现有处理人
	Assignee    string     `json:"assignee,omitempty"`
	Group       string     `json:"group,omitempty"`
	CurrentNode string     `json:"current_node,omitempty"`
	Resolution  string     `json:"resolution,omitempty"`
	SnoozeUntil *time.Time `json:"snooze_until,omitempty"`
	Seq         int64      `json:"seq"`
}

// 任务同步的目标状态，与 internal/task 的取值一一对应（引擎不直接依赖任务中心）。
const (
	SyncUnassigned = "unassigned"
	SyncPending    = "pending"
	SyncSnoozed    = "snoozed"
	SyncSolved     = "solved"
)
