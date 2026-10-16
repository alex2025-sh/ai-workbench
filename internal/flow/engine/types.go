// Package engine 是轻量工作流引擎的执行核心：推进器、认领与租约、边传播、区域重置、任务状态同步。
//
// 核心思想：workflow 是图数据而不是代码，运行状态直接存表，不需要确定性回放。
// 正确性的核心是“事务外执行节点，事务内推进图”，同一个 run 的推进通过锁 wf_run 行串行化。
// 设计见 docs/workflow-engine.md。
package engine

import (
	"encoding/json"
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

// JobKind 是 job 的类型。所有“等待”都落成带 due_at 的 job，由 xxl-job ticker 唤醒。
type JobKind string

const (
	JobExecute  JobKind = "execute"
	JobTimeout  JobKind = "timeout"
	JobWake     JobKind = "wake"
	JobTimer    JobKind = "timer"
	JobSyncTask JobKind = "sync_task" // 引擎内的事务外发：把节点事件投影到任务中心
)

// JobStatus 是 job 的状态。
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobDone      JobStatus = "done"
	JobCancelled JobStatus = "cancelled"
)

// Run 是运行实例的领域视图。
type Run struct {
	ID          int64
	PlanID      string
	PlanVersion int
	BizKey      string
	Status      RunStatus
	Vars        json.RawMessage
	Version     int64 // 每次推进 +1，同时作为 sync_task 的 seq
	ExecCount   int
	DryRun      bool
}

// NodeRun 是节点在当前轮的状态。
type NodeRun struct {
	RunID      int64
	NodeID     string
	Iteration  int
	Status     NodeStatus
	Port       string
	Output     json.RawMessage
	Attempt    int
	WaitUntil  *time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Job 是一条待执行或待触发的工作单元。
type Job struct {
	ID          int64
	RunID       int64
	NodeID      string
	Iteration   int
	Kind        JobKind
	Payload     json.RawMessage
	DueAt       time.Time
	Status      JobStatus
	Attempt     int
	Owner       string
	LockedUntil *time.Time
	IdemKey     string
}

// IdemKey 生成 job 与外部动作的幂等键：run_id:node_id:iteration:kind。
// 带上 iteration，退回后新一轮的同名动作不会被上一轮的幂等记录误拦。
func IdemKey(runID int64, nodeID string, iteration int, kind JobKind) string {
	return formatIdemKey(runID, nodeID, iteration, string(kind))
}
