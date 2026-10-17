// Package engine 是轻量工作流引擎的执行核心：推进器、认领与租约、边传播、任务状态同步。
//
// 核心思想：workflow 是图数据而不是代码，运行状态直接存表，不需要确定性回放。
// 正确性的核心是“事务外执行节点，事务内推进图”，同一个 run 的推进通过锁 wf_run 行串行化。
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

// JobKind 是 job 的类型。所有“等待”都落成带 due_at 的 job。
type JobKind string

const (
	JobExecute  JobKind = "execute"
	JobTimeout  JobKind = "timeout"
	JobWake     JobKind = "wake"
	JobTimer    JobKind = "timer"
	JobSyncTask JobKind = "sync_task" // 事务外发：把节点事件投影到任务中心
)

// JobStatus 是 job 的状态。
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobDone      JobStatus = "done"
	JobCancelled JobStatus = "cancelled"
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
	Version     int64     `json:"version"` // 每次推进 +1，同时作为 sync_task 的 seq
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

// Job 是一条待执行或待触发的工作单元。
type Job struct {
	ID          int64           `json:"id"`
	RunID       int64           `json:"run_id"`
	NodeID      string          `json:"node_id"`
	Iteration   int             `json:"iteration"`
	Kind        JobKind         `json:"kind"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	DueAt       time.Time       `json:"due_at"`
	Status      JobStatus       `json:"status"`
	Attempt     int             `json:"attempt"`
	Owner       string          `json:"owner,omitempty"`
	LockedUntil *time.Time      `json:"locked_until,omitempty"`
	IdemKey     string          `json:"idem_key"`
}

// IdemKey 生成 job 与外部动作的幂等键：run_id:node_id:iteration:kind。
// 带上 iteration，退回后新一轮的同名动作不会被上一轮的幂等记录误拦。
func IdemKey(runID int64, nodeID string, iteration int, kind JobKind) string {
	return strconv.FormatInt(runID, 10) + ":" + nodeID + ":" + strconv.Itoa(iteration) + ":" + string(kind)
}

// SyncPayload 是 sync_task job 的参数，交给任务中心按 seq 幂等应用。
type SyncPayload struct {
	TaskID      int64      `json:"task_id"`
	Status      string     `json:"status"` // unassigned / pending / snoozed / solved
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
