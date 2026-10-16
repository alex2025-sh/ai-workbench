package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

func formatIdemKey(runID int64, nodeID string, iteration int, kind string) string {
	return strconv.FormatInt(runID, 10) + ":" + nodeID + ":" + strconv.Itoa(iteration) + ":" + kind
}

// StartRunCmd 启动一次运行；BizKey 唯一，重复启动返回已有 run。
type StartRunCmd struct {
	PlanID  string
	BizKey  string
	Trigger json.RawMessage
	DryRun  bool
}

// SignalCmd 是外部对等待中人工节点的输入。调用方（API 层）负责先按任务表鉴权。
type SignalCmd struct {
	RunID     int64
	NodeID    string
	Iteration int
	Option    string
	Form      json.RawMessage
	Actor     string
}

// SignalResult 对应 Temporal Update 的语义：先校验，不通过直接拒绝并返回原因。
type SignalResult struct {
	Accepted bool
	Reason   string
}

// RunView 是运行状态的查询视图，供运行态画布着色。
type RunView struct {
	Run   Run
	Nodes []NodeRun
}

// Engine 是引擎对外的 API。
type Engine interface {
	StartRun(ctx context.Context, cmd StartRunCmd) (run Run, created bool, err error)
	Signal(ctx context.Context, cmd SignalCmd) (SignalResult, error)
	Cancel(ctx context.Context, runID int64, actor, reason string) error
	GetRun(ctx context.Context, runID int64) (*RunView, error)

	// 人工干预：重试节点、以指定端口跳过节点。
	RetryNode(ctx context.Context, runID int64, nodeID, actor, reason string) error
	SkipNode(ctx context.Context, runID int64, nodeID, port, actor, reason string) error
}

// NodeResult 是节点执行器在事务外执行后的结果，交给推进器在事务内提交。
type NodeResult struct {
	Port   string          // 选中的输出端口；为空表示进入等待（human_choice / timer）
	Output json.RawMessage // 大输出只放引用
	Wait   *WaitSpec       // 进入等待时插入的 job
	Err    error
}

// WaitSpec 描述节点进入 waiting 时需要插入的定时 job。
type WaitSpec struct {
	TimeoutAt *time.Time // 为空表示不设超时
	WakeAt    *time.Time // 重复提醒或 snooze 唤醒
	TimerAt   *time.Time // timer 节点
}

// Advancer 是唯一修改节点状态的组件。每个方法都在一个悲观事务内完成：
// 锁 run 行 → 防护校验（status / iteration / attempt）→ 写结果 → 边传播与死路径消除
// → 插入新 job（含 sync_task）→ 判断 run 是否结束 → 写 wf_event。
type Advancer interface {
	// Complete 提交一次节点执行的结果。返回提交后新就绪、可立即派发的 job。
	Complete(ctx context.Context, job Job, result NodeResult) (ready []Job, err error)

	// ApplySignal 在事务内校验并应用人工选择。点击与超时在同一行锁下竞争。
	ApplySignal(ctx context.Context, cmd SignalCmd) (SignalResult, []Job, error)

	// Fire 处理到期的 timeout / wake / timer job。
	Fire(ctx context.Context, job Job) ([]Job, error)
}

// Worker 认领并执行 job。
type Worker interface {
	// Claim 用 CAS 认领一个到期 job（TiDB 不支持 SKIP LOCKED）：
	//   UPDATE wf_job SET owner=?, locked_until=?, attempt=attempt+1
	//   WHERE id=? AND status='pending' AND due_at<=NOW() AND (owner='' OR locked_until<NOW())
	Claim(ctx context.Context, jobID int64, owner string, lease time.Duration) (bool, error)

	// Dispatch 把提交后新就绪的 job 直接交给本进程的执行池；ticker 只做兜底。
	Dispatch(jobs []Job)
}

// Clock 抽象时间来源。试运行使用虚拟时钟，让超时与提醒即时快进。
type Clock interface {
	Now() time.Time
}
