package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

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
	Config    any                       // dsl.DecodeConfig 解码后的配置，只读
	Binding   *compiler.ResolvedBinding // human_choice / lark_card 的模板与分配策略
	Iteration int
	Attempt   int    // 本轮第几次执行，从 1 开始
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
	// Defer 为 true 时只提交启动，不就地推进：新就绪的 job 由随后的 tick 执行。
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
	Accepted bool       `json:"accepted"`
	Code     RejectCode `json:"code,omitempty"`   // 拒绝原因分类，调用方据此决定刷新还是留在表单
	Reason   string     `json:"reason,omitempty"` // 给人看的原因，卡片与页面上即时提示
}

// RejectCode 是 Signal 被拒绝的原因分类，与 SignalResp.code 一致。
type RejectCode string

const (
	RejectNotWaiting     RejectCode = "not_waiting"     // 流程已结束或已取消
	RejectStale          RejectCode = "stale"           // 节点或 iteration 已不是当前等待的节点
	RejectAlreadyHandled RejectCode = "already_handled" // 该节点已被处理（如在 Lark 点过）
	RejectInvalidForm    RejectCode = "invalid_form"    // 未知选项或必填字段缺失
	RejectUnsupported    RejectCode = "unsupported"     // 选项暂不可用
)

// RunView 是运行状态的查询视图，供运行态画布着色。
type RunView struct {
	Run   Run       `json:"run"`
	Nodes []NodeRun `json:"nodes"`
}

// Options 是引擎的运行参数。
type Options struct {
	Owner        string // 本进程标识，写入 wf_job.owner，只用于排查
	MaxExecCount int    // 单个 run 的节点执行次数上限，默认 500，超出进入人工干预
	MaxFailCount int    // job 处理出错（加载、提交出错）的次数上限，默认 5，超出后隔离

	// 推进的边界：一次推进最多执行 DriveBudget 个 job（默认 100），同时最多 DriveParallelism 个（默认 4）；
	// 超出预算的 job 留在表里由 tick 接着执行。
	DriveBudget      int
	DriveParallelism int
	// BackgroundLimit 是同时进行的后台推进数上限（默认 64），满了就不再起后台推进，留给 tick。
	BackgroundLimit int
	// BackgroundTimeout 是一次后台推进的时长上限（默认 10 分钟）。
	BackgroundTimeout time.Duration
}

// 单次执行的超时：skill_call / llm 可在配置中覆盖，上限 MaxActionTimeout。
// 认领时租约设为“超时 + leaseMargin”，执行期间不会被重复认领；进程崩溃时租约到期后由 tick 接手。
const (
	DefaultActionTimeout = 30 * time.Second
	DefaultLLMTimeout    = 2 * time.Minute
	MaxActionTimeout     = 10 * time.Minute
	leaseMargin          = 30 * time.Second // 超时之后租约再保留的余量，覆盖提交结果的时间
	commitTimeout        = 10 * time.Second // 提交执行结果的时长上限，不受调用方取消的影响
)

// ExecTimeout 返回节点单次执行的超时。
func ExecTimeout(cfg any) time.Duration {
	d := DefaultActionTimeout
	switch c := cfg.(type) {
	case *dsl.SkillCallConfig:
		if c.Timeout != nil && *c.Timeout > 0 {
			d = c.Timeout.Std()
		}
	case *dsl.LLMConfig:
		d = DefaultLLMTimeout
		if c.Timeout != nil && *c.Timeout > 0 {
			d = c.Timeout.Std()
		}
	}
	if d > MaxActionTimeout {
		d = MaxActionTimeout
	}
	return d
}

// Engine 是引擎对外的 API，同时是唯一修改流程状态的组件。
//
// 所有状态修改都经 update 在锁住 run 行的事务内完成，包括 job 的认领与结束；外部 IO 只在事务之间执行。
// 引擎是事件驱动的：StartRun（来源产生新数据）、Signal（人工操作）、Cancel（外部状态变化）
// 与 ProcessDue（xxl-job 的 tick）是仅有的入口，每个入口提交后就地执行新就绪的 job（见 drive.go）。
type Engine struct {
	store   *Store
	plans   PlanSource
	execs   Executors
	actions Invoker
	clock   Clock
	opts    Options

	// 后台推进（见 driveLater）：有并发上限，进程退出时由 Close 等待或取消。
	bgSem  chan struct{}
	bgWG   sync.WaitGroup
	bgCtx  context.Context
	bgStop context.CancelFunc
}

// New 创建引擎。
func New(store *Store, plans PlanSource, execs Executors, actions Invoker, clock Clock, opts Options) *Engine {
	if opts.Owner == "" {
		opts.Owner = "engine"
	}
	if opts.MaxExecCount <= 0 {
		opts.MaxExecCount = 500
	}
	if opts.MaxFailCount <= 0 {
		opts.MaxFailCount = 5
	}
	if opts.DriveBudget <= 0 {
		opts.DriveBudget = 100
	}
	if opts.DriveParallelism <= 0 {
		opts.DriveParallelism = 4
	}
	if opts.BackgroundLimit <= 0 {
		opts.BackgroundLimit = 64
	}
	if opts.BackgroundTimeout <= 0 {
		opts.BackgroundTimeout = 10 * time.Minute
	}
	bg, stop := context.WithCancel(context.Background())
	return &Engine{store: store, plans: plans, execs: execs, actions: actions, clock: clock, opts: opts,
		bgSem: make(chan struct{}, opts.BackgroundLimit), bgCtx: bg, bgStop: stop}
}

// Store 返回底层存储，供查询使用。
func (e *Engine) Store() *Store { return e.store }

// GetRun 返回 run 及其节点状态。
func (e *Engine) GetRun(ctx context.Context, runID int64) (*RunView, error) {
	return e.store.GetRun(ctx, runID)
}

// StartRun 启动一次运行：插入 run 与全部节点，完成 trigger 并传播。BizKey 相同的 run 已存在时直接返回它。
func (e *Engine) StartRun(ctx context.Context, cmd StartRunCmd) (Run, bool, error) {
	if existing, err := e.store.findRunByBizKey(ctx, cmd.BizKey); err != nil || existing != nil {
		if existing != nil {
			return *existing, false, nil
		}
		return Run{}, false, err
	}
	def, err := e.plans.Load(ctx, cmd.PlanKey, cmd.PlanVersion)
	if err != nil {
		return Run{}, false, err
	}
	type started struct {
		run  Run
		jobs []Job
	}
	out, err := transact(ctx, e.store, func(t *tx) (started, error) {
		run := Run{PlanKey: cmd.PlanKey, PlanVersion: cmd.PlanVersion, BizKey: cmd.BizKey, Status: RunRunning, Vars: cmd.Vars, DryRun: cmd.DryRun}
		row, err := runToRow(run)
		if err != nil {
			return started{}, err
		}
		res, err := t.runs.Insert(t.ctx, row)
		if err != nil {
			return started{}, err
		}
		if run.ID, err = res.LastInsertId(); err != nil {
			return started{}, err
		}
		s := newRunState(t, run, def, e.clock.Now())
		for _, id := range def.Plan.Order {
			s.nodes[id] = &model.WfNodeRun{RunId: uint64(run.ID), NodeId: id, Status: string(NodePending)}
		}
		s.event("", "run_started", "", map[string]any{"plan": cmd.PlanKey, "version": cmd.PlanVersion, "biz_key": cmd.BizKey})
		s.setNode(def.Plan.Entry, NodeRunning, func(r *model.WfNodeRun) { r.StartedAt = s.at() })
		s.complete(def.Plan.Entry, dsl.PortNext, nil)
		jobs, err := s.flush()
		return started{run: s.run, jobs: jobs}, err
	})
	if err != nil {
		if isDuplicate(err) {
			existing, ferr := e.store.findRunByBizKey(ctx, cmd.BizKey)
			if ferr == nil && existing != nil {
				return *existing, false, nil
			}
		}
		return Run{}, false, err
	}
	if !cmd.Defer {
		e.drive(ctx, out.jobs)
	}
	return out.run, true, nil
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

// Signal 在事务内校验并应用人工选择。点击与超时在同一行锁下竞争，只有先到的生效。
func (e *Engine) Signal(ctx context.Context, cmd SignalCmd) (SignalResult, error) {
	res, jobs, err := update(ctx, e, cmd.RunID, 0, func(s *runState) (SignalResult, error) {
		return s.applySignal(cmd), nil
	})
	if err != nil {
		return SignalResult{}, err
	}
	if cmd.Async {
		e.driveLater(ctx, jobs)
	} else {
		e.drive(ctx, jobs)
	}
	return res, nil
}

// applySignal 校验选择；通过时完成人工节点并传播，不通过时不做任何修改。
func (s *runState) applySignal(cmd SignalCmd) SignalResult {
	reject := func(code RejectCode, reason string) SignalResult { return SignalResult{Code: code, Reason: reason} }
	r := s.nodes[cmd.NodeID]
	n, cfg := s.def.node(cmd.NodeID)
	switch {
	case s.run.Status != RunRunning:
		return reject(RejectNotWaiting, "流程已结束")
	case r == nil || n.Type != dsl.NodeHumanChoice:
		return reject(RejectStale, "节点不存在或不是人工选择节点")
	case NodeStatus(r.Status) != NodeWaiting:
		return reject(RejectAlreadyHandled, "该节点已处理")
	case int(r.Iteration) != cmd.Iteration:
		return reject(RejectStale, "卡片已过期，请查看最新卡片")
	}
	var opt *dsl.Option
	for i, o := range cfg.(*dsl.HumanChoiceConfig).Options {
		if o.Key == cmd.Option {
			opt = &cfg.(*dsl.HumanChoiceConfig).Options[i]
		}
	}
	if opt == nil {
		return reject(RejectInvalidForm, fmt.Sprintf("未知选项 %q", cmd.Option))
	}
	for _, f := range opt.RequiredFields {
		if strings.TrimSpace(cmd.Form[f]) == "" {
			return reject(RejectInvalidForm, fmt.Sprintf("请填写 %s", f))
		}
	}
	if opt.Snooze != nil {
		s.snooze(cmd, opt.Snooze.Std())
		return SignalResult{Accepted: true}
	}
	s.cancelJobs(cmd.NodeID, JobTimeout, JobWake)
	s.event(cmd.NodeID, "signal", cmd.Actor, map[string]any{"option": cmd.Option, "form": cmd.Form})
	// 等待时的输出是处理人与发出的卡片；做出选择后处理人归入 assignment，卡片保留在顶层供任务详情显示。
	prev, _ := expr.Decode([]byte(nullOr(r.Output)))
	result := map[string]any{"option": cmd.Option, "form": cmd.Form, "actor": cmd.Actor, "assignment": prev}
	if m, ok := prev.(map[string]any); ok {
		if card, ok := m["card"]; ok {
			delete(m, "card")
			result["card"] = card
		}
	}
	out, _ := json.Marshal(result)
	// 做决定的人成为任务的处理人：组任务没人认领时也能记下是谁处理的。
	if cmd.Actor != "" {
		s.assign(cmd.NodeID, &Assignment{Assignee: cmd.Actor})
	}
	s.complete(cmd.NodeID, cmd.Option, out)
	return SignalResult{Accepted: true}
}

// Cancel 取消 run：撤销尚未执行的节点与定时，进行中（就绪、执行中、等待中）的节点置为 cancelled，
// 任务置为 solved（auto_closed）。已结束的 run 不受影响。
func (e *Engine) Cancel(ctx context.Context, runID int64, actor, reason string) error {
	_, jobs, err := update(ctx, e, runID, 0, func(s *runState) (struct{}, error) {
		if s.run.Status.Terminal() {
			return struct{}{}, nil
		}
		s.setRunStatus(RunCancelled)
		s.cancelJobs("", graphJobs...)
		for _, id := range s.def.Plan.Order {
			switch NodeStatus(s.nodes[id].Status) {
			case NodeReady, NodeRunning, NodeWaiting:
				s.setNode(id, NodeCancelled, func(r *model.WfNodeRun) {
					r.FinishedAt = s.at()
					r.WaitUntil = sql.NullTime{}
				})
			}
		}
		s.sync = SyncSolved
		s.event("", "run_cancelled", actor, map[string]any{"reason": reason})
		return struct{}{}, nil
	})
	if err == nil {
		e.drive(ctx, jobs) // 执行任务同步
	}
	return err
}
