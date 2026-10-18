package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

// Definition 是一个已发布的任务类型版本：完整定义与编译后的执行计划。发布后不可变。
type Definition struct {
	Key     string
	Version int
	Spec    *spec.TaskType
	Plan    *compiler.Plan
}

// PlanSource 按 (任务类型, 版本) 加载已发布的定义。
type PlanSource interface {
	Load(ctx context.Context, key string, version int) (*Definition, error)
}

// Invoker 是动作运行时，所有外部 IO 都经它调用（见 internal/action）。
type Invoker interface {
	InvokeAction(ctx context.Context, req action.Request) (action.Result, error)
}

// Clock 抽象时间来源。推进逻辑只从这里取时间；试运行和测试使用可快进的时钟。
type Clock interface {
	Now() time.Time
}

// ExecContext 是节点在事务外执行时可见的上下文。
type ExecContext struct {
	Run       Run
	Def       *Definition
	Node      dsl.Node
	Config    any                       // dsl.DecodeConfig 解码后的配置
	Binding   *compiler.ResolvedBinding // human_choice / lark_card 的模板与分配策略
	Iteration int
	Attempt   int
	IdemKey   string // 外部动作的幂等键 run_id:node_id:iteration
	Env       expr.Env
	Now       time.Time
	Actions   Invoker
}

// Assignment 是节点确定的处理人，用于更新任务的 assignee。
type Assignment struct {
	Assignee string `json:"assignee,omitempty"`
	Group    string `json:"group,omitempty"`
}

// NodeResult 是节点执行器在事务外执行后的结果，由推进器在事务内提交。
//   - Err 非空：执行失败，按重试策略重试；Permanent 为 true 时不重试。
//   - Wait 非空：节点进入等待（timer / human_choice）。
//   - 否则：节点完成，选中 Port（end 节点没有端口）。
type NodeResult struct {
	Port       string
	Output     json.RawMessage
	Wait       *WaitSpec
	Assignment *Assignment
	Effects    []Effect // 节点结果提交之后才执行的外部动作
	Err        error
	Permanent  bool
}

// Effect 是一个要在节点结果提交之后执行的外部动作。它与节点结果在同一事务里落成 effect job，
// 提交后立即执行，失败按退避重试。
//
// 人工节点的卡片走这条路：先提交 waiting，再发卡片。这样处理人看到卡片时节点一定已在等待，点击不会被误拒；
// 发送失败也只重试发送，不会重新执行整个节点。
type Effect struct {
	Kind         action.Kind
	Config       json.RawMessage
	Input        json.RawMessage
	Key          string // 幂等键后缀，与节点的幂等键拼成外部动作的幂等键
	WhileWaiting bool   // 只在节点仍在等待时执行
}

// WaitSpec 描述节点进入 waiting 时需要插入的定时 job。
type WaitSpec struct {
	TimerAt   *time.Time // timer 节点到期后走 next
	TimeoutAt *time.Time // human_choice 超时后走 timeout
}

// Executor 是一种节点类型的运行时实现，见 internal/flow/nodes。
type Executor interface {
	Type() dsl.NodeType
	Execute(ctx context.Context, ec ExecContext) NodeResult
}

// Executors 按节点类型查找执行器。
type Executors interface {
	Get(t dsl.NodeType) (Executor, bool)
}

// StartRunCmd 启动一次运行；BizKey 唯一，重复启动返回已有 run。
type StartRunCmd struct {
	PlanKey     string
	PlanVersion int
	BizKey      string
	Vars        Vars
	DryRun      bool
	// Defer 为 true 时只提交启动，不就地推进：新就绪的 job 由随后的 tick 并行执行。
	// cron 来源一次扫描可能启动很多 run，逐个就地推进会把 tick 拖长。
	Defer bool
}

// SignalCmd 是外部对等待中人工节点的输入。调用方（API 层）负责先按任务表鉴权。
type SignalCmd struct {
	RunID     int64
	NodeID    string
	Iteration int
	Option    string
	Form      map[string]string
	Actor     string
	// Async 为 true 时信号提交后立即返回，后续推进在后台进行（Lark 回调要求 3 秒内响应）。
	Async bool
}

// SignalResult 对应 Temporal Update 的语义：先校验，不通过直接拒绝并返回原因。
type SignalResult struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

// RunView 是运行状态的查询视图，供运行态画布着色。
type RunView struct {
	Run   Run       `json:"run"`
	Nodes []NodeRun `json:"nodes"`
}
