package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

// Options 是引擎的运行参数。
type Options struct {
	Owner        string        // 本进程标识，写入 wf_job.owner
	Lease        time.Duration // job 租约，默认 2 分钟
	MaxExecCount int           // 单个 run 的节点执行次数上限，默认 500，超出进入人工干预
}

// Engine 是引擎对外的 API，同时是唯一修改节点状态的组件（推进器）。
// 每次推进都在一个事务内完成：锁 run 行 → 防护校验 → 写结果 → 边传播与死路径消除 → 插入新 job → 判断结束 → 写事件。
//
// 引擎是事件驱动的，没有常驻的轮询者：StartRun（来源产生新数据）、Signal（人工操作）、Cancel（外部状态变化）
// 与 ProcessDue（xxl-job 的 tick，唤醒到期的定时器、超时与重试）是仅有的入口。每个入口提交后就地往下执行
// 新就绪的节点，直到遇到等待点或结束（见 drive.go）。
type Engine struct {
	store   *Store
	plans   PlanSource
	execs   Executors
	actions Invoker
	clock   Clock
	opts    Options
}

// New 创建引擎。
func New(store *Store, plans PlanSource, execs Executors, actions Invoker, clock Clock, opts Options) *Engine {
	if opts.Lease <= 0 {
		opts.Lease = 2 * time.Minute
	}
	if opts.MaxExecCount <= 0 {
		opts.MaxExecCount = 500
	}
	if opts.Owner == "" {
		opts.Owner = "engine"
	}
	return &Engine{store: store, plans: plans, execs: execs, actions: actions, clock: clock, opts: opts}
}

// Store 返回底层存储，供查询使用。
func (e *Engine) Store() *Store { return e.store }

// errLostLease 表示 job 已被其他 worker 重新认领，本次提交放弃。
var errLostLease = errors.New("engine: job lease lost")

// ---- 一次推进的事务内状态 ----

type runState struct {
	e     *Engine
	t     *tx
	run   Run
	def   *Definition
	rows  map[string]*model.WfNodeRun
	modes map[string]dsl.MergeMode
	now   time.Time
	dirty map[string]bool
	jobs  []Job // 本事务新插入的 job，提交后派发

	sync    string // 非空时提交前插入一个 sync_task job，目标状态见 Sync*
	current string // 任务当前所处的节点
}

func (e *Engine) load(ctx context.Context, t *tx, runID int64) (*runState, error) {
	run, rows, err := t.lockRun(runID)
	if err != nil {
		return nil, err
	}
	def, err := e.plans.Load(ctx, run.PlanKey, run.PlanVersion)
	if err != nil {
		return nil, err
	}
	run.Version++ // 本次推进的版本，同时作为 sync_task 的 seq
	return &runState{e: e, t: t, run: run, def: def, rows: rows, modes: mergeModes(def.Plan), now: e.clock.Now(), dirty: map[string]bool{}}, nil
}

func (s *runState) states() map[string]*state {
	st := make(map[string]*state, len(s.rows))
	for id, r := range s.rows {
		st[id] = &state{Status: NodeStatus(r.Status), Port: r.Port}
	}
	return st
}

func (s *runState) setNode(id string, status NodeStatus, mut func(r *model.WfNodeRun)) {
	r := s.rows[id]
	r.Status = string(status)
	if mut != nil {
		mut(r)
	}
	s.dirty[id] = true
}

func (s *runState) addJob(nodeID string, kind JobKind, due time.Time, payload json.RawMessage, keySuffix string) error {
	iter := 0
	if r := s.rows[nodeID]; r != nil {
		iter = int(r.Iteration)
	}
	key := IdemKey(s.run.ID, nodeID, iter, kind) + keySuffix
	j, err := s.t.insertJob(Job{RunID: s.run.ID, NodeID: nodeID, Iteration: iter, Kind: kind, DueAt: due, Payload: payload, IdemKey: key})
	if err != nil {
		return err
	}
	if j != nil {
		s.jobs = append(s.jobs, *j)
	}
	return nil
}

func (s *runState) event(nodeID, typ, actor string, payload any) error {
	iter := 0
	if r := s.rows[nodeID]; r != nil {
		iter = int(r.Iteration)
	}
	return s.t.event(s.run.ID, nodeID, iter, typ, actor, payload)
}

// assign 记录节点确定的处理人，并在提交时同步任务（pending 或待认领）。
func (s *runState) assign(nodeID string, a *Assignment) {
	if a == nil {
		return
	}
	s.run.Vars.Task.Assignee, s.run.Vars.Task.Group = a.Assignee, a.Group
	if a.Assignee != "" {
		s.run.Vars.Task.Group = ""
		s.sync = SyncPending
	} else {
		s.sync = SyncUnassigned
	}
	s.current = nodeID
}

// complete 把节点标记为成功并传播；所有节点都有结论时结束 run。
func (s *runState) complete(nodeID, port string, output json.RawMessage) error {
	s.setNode(nodeID, NodeSucceeded, func(r *model.WfNodeRun) {
		r.Port = port
		r.Output = nullJSON(output)
		r.FinishedAt = sql.NullTime{Time: s.now, Valid: true}
		r.WaitUntil = sql.NullTime{}
	})
	if err := s.event(nodeID, "node_completed", "", map[string]any{"port": port}); err != nil {
		return err
	}
	st := s.states()
	ready, skipped := propagate(s.def.Plan, s.modes, st, nodeID)
	for _, id := range skipped {
		s.setNode(id, NodeSkipped, func(r *model.WfNodeRun) { r.FinishedAt = sql.NullTime{Time: s.now, Valid: true} })
		if err := s.event(id, "node_skipped", "", nil); err != nil {
			return err
		}
	}
	for _, id := range ready {
		s.setNode(id, NodeReady, nil)
		if err := s.addJob(id, JobExecute, s.now, nil, ""); err != nil {
			return err
		}
	}
	if allConcluded(s.def.Plan, s.states()) {
		return s.finish(RunSucceeded, s.resolution())
	}
	return nil
}

// resolution 取第一个成功结束的 end 节点上配置的结论，否则取最后一个人工节点的选项。
func (s *runState) resolution() string {
	last := ""
	for _, id := range s.def.Plan.Order {
		r := s.rows[id]
		if NodeStatus(r.Status) != NodeSucceeded {
			continue
		}
		n := s.node(id)
		switch n.Type {
		case dsl.NodeEnd:
			if cfg, err := dsl.DecodeConfig(n.Type, n.Config); err == nil && cfg.(*dsl.EndConfig).Resolution != "" {
				return cfg.(*dsl.EndConfig).Resolution
			}
		case dsl.NodeHumanChoice:
			last = r.Port
		}
	}
	if last == "" {
		return "completed"
	}
	return last
}

func (s *runState) node(id string) dsl.Node {
	for _, n := range s.def.Plan.Graph.Nodes {
		if n.ID == id {
			return n
		}
	}
	return dsl.Node{}
}

func (s *runState) finish(status RunStatus, resolution string) error {
	s.run.Status = status
	if err := s.t.jobs.CancelRun(s.t.ctx, uint64(s.run.ID)); err != nil {
		return err
	}
	s.sync = SyncSolved
	return s.event("", "run_"+string(status), "", map[string]any{"resolution": resolution})
}

func (s *runState) needsIntervention(nodeID, reason string) error {
	s.run.Status = RunNeedsIntervention
	return s.event(nodeID, "run_needs_intervention", "", map[string]any{"reason": reason})
}

// commit 写回 run 与变化的节点，并插入任务同步 job。
func (s *runState) commit() error {
	for _, id := range s.def.Plan.Order { // 按拓扑序写回，顺序稳定
		if s.dirty[id] {
			if err := s.t.saveNode(s.rows[id]); err != nil {
				return err
			}
		}
	}
	if s.sync != "" && s.run.Vars.Task.ID != 0 {
		p := SyncPayload{
			TaskID: s.run.Vars.Task.ID, Status: s.sync, Assignee: s.run.Vars.Task.Assignee, Group: s.run.Vars.Task.Group,
			CurrentNode: s.current, Seq: s.run.Version,
		}
		if s.sync == SyncSolved {
			p.Resolution = s.resolution()
			if s.run.Status == RunCancelled {
				p.Resolution = "auto_closed"
			}
		}
		b, _ := json.Marshal(p)
		if err := s.addJob("", JobSyncTask, s.now, b, fmt.Sprintf(":%d", s.run.Version)); err != nil {
			return err
		}
	}
	return s.t.saveRun(s.run)
}

// ---- 启动 ----

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
	var started Run
	var jobs []Job
	err = e.store.Tx(ctx, func(t *tx) error {
		run := Run{PlanKey: cmd.PlanKey, PlanVersion: cmd.PlanVersion, BizKey: cmd.BizKey, Status: RunRunning, Vars: cmd.Vars, Version: 1, DryRun: cmd.DryRun}
		row, err := runToRow(run, 0)
		if err != nil {
			return err
		}
		res, err := t.runs.Insert(t.ctx, row)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		run.ID = id
		now := e.clock.Now()
		rows := map[string]*model.WfNodeRun{}
		for _, nid := range def.Plan.Order {
			n := &model.WfNodeRun{RunId: uint64(id), NodeId: nid, Status: string(NodePending)}
			r, err := t.nodes.Insert(t.ctx, n)
			if err != nil {
				return err
			}
			nrid, _ := r.LastInsertId()
			n.Id = uint64(nrid)
			rows[nid] = n
		}
		s := &runState{e: e, t: t, run: run, def: def, rows: rows, modes: mergeModes(def.Plan), now: now, dirty: map[string]bool{}}
		if err := s.event("", "run_started", "", map[string]any{"plan": cmd.PlanKey, "version": cmd.PlanVersion, "biz_key": cmd.BizKey}); err != nil {
			return err
		}
		s.setNode(def.Plan.Entry, NodeRunning, func(r *model.WfNodeRun) { r.StartedAt = sql.NullTime{Time: now, Valid: true} })
		if err := s.complete(def.Plan.Entry, dsl.PortNext, nil); err != nil {
			return err
		}
		if err := s.commit(); err != nil {
			return err
		}
		started, jobs = s.run, s.jobs
		return nil
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
	e.drive(ctx, jobs)
	return started, true, nil
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

// ---- 执行 job ----

// process 处理一个已认领的 job，返回处理过程中新插入的 job。
func (e *Engine) process(ctx context.Context, job Job) ([]Job, error) {
	switch job.Kind {
	case JobExecute:
		return e.execute(ctx, job)
	case JobTimer, JobTimeout:
		return e.fire(ctx, job)
	case JobSyncTask:
		return nil, e.syncTask(ctx, job)
	default:
		_, err := e.store.jobs.Finish(ctx, uint64(job.ID), job.Owner, string(JobCancelled), "unsupported job kind "+string(job.Kind))
		return nil, err
	}
}

func (e *Engine) finishStale(t *tx, job Job, reason string) error {
	_, err := t.jobs.Finish(t.ctx, uint64(job.ID), job.Owner, string(JobCancelled), reason)
	return err
}

func (e *Engine) execute(ctx context.Context, job Job) ([]Job, error) {
	// 第一段：事务内把节点置为 running，记下本次尝试的 attempt，并准备执行上下文。
	var ec *ExecContext
	err := e.store.Tx(ctx, func(t *tx) error {
		s, err := e.load(ctx, t, job.RunID)
		if err != nil {
			return err
		}
		r := s.rows[job.NodeID]
		if s.run.Status != RunRunning || r == nil || int(r.Iteration) != job.Iteration ||
			(NodeStatus(r.Status) != NodeReady && NodeStatus(r.Status) != NodeRunning) {
			return e.finishStale(t, job, "stale")
		}
		if s.run.ExecCount >= e.opts.MaxExecCount {
			if err := s.needsIntervention(job.NodeID, "exceeded max exec count"); err != nil {
				return err
			}
			if err := e.finishStale(t, job, "max exec count"); err != nil {
				return err
			}
			return s.commit()
		}
		s.run.ExecCount++
		s.setNode(job.NodeID, NodeRunning, func(r *model.WfNodeRun) {
			r.Attempt = int64(job.Attempt)
			r.StartedAt = sql.NullTime{Time: s.now, Valid: true}
		})
		if err := s.event(job.NodeID, "node_started", "", map[string]any{"attempt": job.Attempt}); err != nil {
			return err
		}
		n := s.node(job.NodeID)
		cfg, err := dsl.DecodeConfig(n.Type, n.Config)
		if err != nil {
			return err
		}
		var binding = s.def.Plan.Bindings[job.NodeID]
		ec = &ExecContext{
			Run: s.run, Def: s.def, Node: n, Config: cfg, Iteration: job.Iteration, Attempt: job.Attempt,
			IdemKey: fmt.Sprintf("%d:%s:%d", s.run.ID, job.NodeID, job.Iteration),
			Env:     buildEnv(s.run, s.rows, job.Iteration), Now: s.now, Actions: e.actions,
		}
		if _, ok := s.def.Plan.Bindings[job.NodeID]; ok {
			ec.Binding = &binding
		}
		return s.commit()
	})
	if err != nil || ec == nil {
		return nil, err
	}

	// 第二段：事务外执行节点，不持有任何数据库锁。
	var res NodeResult
	if ex, ok := e.execs.Get(ec.Node.Type); ok {
		res = ex.Execute(ctx, *ec)
	} else {
		res = NodeResult{Err: fmt.Errorf("no executor for node type %q", ec.Node.Type), Permanent: true}
	}

	// 第三段：事务内防护校验后提交结果。
	var jobs []Job
	err = e.store.Tx(ctx, func(t *tx) error {
		s, err := e.load(ctx, t, job.RunID)
		if err != nil {
			return err
		}
		r := s.rows[job.NodeID]
		if s.run.Status != RunRunning || r == nil || NodeStatus(r.Status) != NodeRunning ||
			int(r.Attempt) != job.Attempt || int(r.Iteration) != job.Iteration {
			return e.finishStale(t, job, "fenced") // 僵尸 worker 或 run 已取消，结果丢弃
		}
		if res.Err != nil {
			if err := e.handleFailure(s, job, res); err != nil {
				return err
			}
			jobs = s.jobs // 走 error 端口时有新就绪的节点
			return nil
		}
		if ok, err := t.jobs.Finish(t.ctx, uint64(job.ID), job.Owner, string(JobDone), ""); err != nil {
			return err
		} else if !ok {
			return errLostLease
		}
		if res.Wait != nil {
			if err := s.wait(job.NodeID, res); err != nil {
				return err
			}
		} else {
			s.assign(job.NodeID, res.Assignment)
			if err := s.complete(job.NodeID, res.Port, res.Output); err != nil {
				return err
			}
		}
		if err := s.commit(); err != nil {
			return err
		}
		jobs = s.jobs
		return nil
	})
	if errors.Is(err, errLostLease) {
		return nil, nil
	}
	return jobs, err
}

// wait 让节点进入等待并插入对应的定时 job。
func (s *runState) wait(nodeID string, res NodeResult) error {
	var until *time.Time
	if res.Wait.TimerAt != nil {
		until = res.Wait.TimerAt
		if err := s.addJob(nodeID, JobTimer, *res.Wait.TimerAt, nil, ""); err != nil {
			return err
		}
	}
	if res.Wait.TimeoutAt != nil {
		until = res.Wait.TimeoutAt
		if err := s.addJob(nodeID, JobTimeout, *res.Wait.TimeoutAt, nil, ""); err != nil {
			return err
		}
	}
	s.setNode(nodeID, NodeWaiting, func(r *model.WfNodeRun) {
		r.Output = nullJSON(res.Output)
		r.WaitUntil = toNullTime(until)
	})
	s.assign(nodeID, res.Assignment)
	return s.event(nodeID, "node_waiting", "", map[string]any{"until": until})
}

func (e *Engine) handleFailure(s *runState, job Job, res NodeResult) error {
	n := s.node(job.NodeID)
	policy := RetryPolicyFor(n)
	msg := res.Err.Error()
	if !res.Permanent && job.Attempt < policy.MaxAttempts {
		due := s.now.Add(backoff(policy, job.Attempt))
		if ok, err := s.t.jobs.Release(s.t.ctx, uint64(job.ID), job.Owner, due, msg); err != nil {
			return err
		} else if !ok {
			return errLostLease
		}
		s.setNode(job.NodeID, NodeReady, nil)
		if err := s.event(job.NodeID, "node_retry", "", map[string]any{"attempt": job.Attempt, "error": msg, "next_at": due}); err != nil {
			return err
		}
		return s.commit()
	}
	if ok, err := s.t.jobs.Finish(s.t.ctx, uint64(job.ID), job.Owner, string(JobDone), msg); err != nil {
		return err
	} else if !ok {
		return errLostLease
	}
	if hasEdge(s, job.NodeID, dsl.PortError) {
		out, _ := json.Marshal(map[string]string{"error": msg})
		if err := s.complete(job.NodeID, dsl.PortError, out); err != nil {
			return err
		}
		return s.commit()
	}
	s.setNode(job.NodeID, NodeFailed, func(r *model.WfNodeRun) {
		r.FinishedAt = sql.NullTime{Time: s.now, Valid: true}
		out, _ := json.Marshal(map[string]string{"error": msg})
		r.Output = nullJSON(out)
	})
	if err := s.event(job.NodeID, "node_failed", "", map[string]any{"error": msg, "attempt": job.Attempt}); err != nil {
		return err
	}
	if err := s.needsIntervention(job.NodeID, msg); err != nil {
		return err
	}
	return s.commit()
}

func hasEdge(s *runState, nodeID, port string) bool {
	for _, e := range s.def.Plan.Out[nodeID] {
		if e.Port == port {
			return true
		}
	}
	return false
}

// RetryPolicyFor 返回节点的重试策略：动作节点（含 human_choice）默认最多 5 次、指数退避；
// 原生节点不重试（失败是确定性的，如表达式错误）。DSL 中的 retry 覆盖默认值。
func RetryPolicyFor(n dsl.Node) dsl.RetryPolicy {
	p := dsl.RetryPolicy{MaxAttempts: 1}
	if info, ok := dsl.LookupNodeType(n.Type); ok && (info.Category == dsl.CategoryAction || n.Type == dsl.NodeHumanChoice) {
		p = dsl.RetryPolicy{InitialInterval: dsl.Duration(2 * time.Second), BackoffCoefficient: 2, MaxInterval: dsl.Duration(time.Minute), MaxAttempts: 5}
	}
	if r := n.Retry; r != nil {
		if r.InitialInterval > 0 {
			p.InitialInterval = r.InitialInterval
		}
		if r.BackoffCoefficient > 0 {
			p.BackoffCoefficient = r.BackoffCoefficient
		}
		if r.MaxInterval > 0 {
			p.MaxInterval = r.MaxInterval
		}
		if r.MaxAttempts > 0 {
			p.MaxAttempts = r.MaxAttempts
		}
	}
	return p
}

func backoff(p dsl.RetryPolicy, attempt int) time.Duration {
	coef := p.BackoffCoefficient
	if coef < 1 {
		coef = 1
	}
	d := time.Duration(float64(p.InitialInterval) * math.Pow(coef, float64(attempt-1)))
	if p.MaxInterval > 0 && d > p.MaxInterval.Std() {
		d = p.MaxInterval.Std()
	}
	if d <= 0 {
		d = time.Second
	}
	return d
}

// ---- 定时 job ----

func (e *Engine) fire(ctx context.Context, job Job) ([]Job, error) {
	var jobs []Job
	err := e.store.Tx(ctx, func(t *tx) error {
		s, err := e.load(ctx, t, job.RunID)
		if err != nil {
			return err
		}
		if ok, err := t.jobs.Finish(t.ctx, uint64(job.ID), job.Owner, string(JobDone), ""); err != nil {
			return err
		} else if !ok {
			return errLostLease
		}
		r := s.rows[job.NodeID]
		if s.run.Status != RunRunning || r == nil || NodeStatus(r.Status) != NodeWaiting || int(r.Iteration) != job.Iteration {
			return nil // 节点已被点击或已退回，定时作废
		}
		port := dsl.PortNext
		if job.Kind == JobTimeout {
			port = dsl.PortTimeout
		}
		if err := t.jobs.CancelPending(t.ctx, uint64(s.run.ID), job.NodeID, r.Iteration, []string{string(JobTimeout), string(JobWake), string(JobTimer)}); err != nil {
			return err
		}
		if err := s.complete(job.NodeID, port, json.RawMessage(nullOr(r.Output))); err != nil {
			return err
		}
		if err := s.commit(); err != nil {
			return err
		}
		jobs = s.jobs
		return nil
	})
	if errors.Is(err, errLostLease) {
		return nil, nil
	}
	return jobs, err
}

func nullOr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// ---- 任务同步 ----

func (e *Engine) syncTask(ctx context.Context, job Job) error {
	_, err := e.actions.InvokeAction(ctx, action.Request{Kind: action.KindSyncTask, Input: job.Payload, IdemKey: job.IdemKey})
	if err != nil {
		policy := dsl.RetryPolicy{InitialInterval: dsl.Duration(2 * time.Second), BackoffCoefficient: 2, MaxInterval: dsl.Duration(5 * time.Minute)}
		_, rerr := e.store.jobs.Release(ctx, uint64(job.ID), job.Owner, e.clock.Now().Add(backoff(policy, job.Attempt)), err.Error())
		return rerr
	}
	_, err = e.store.jobs.Finish(ctx, uint64(job.ID), job.Owner, string(JobDone), "")
	return err
}

// ---- 信号与取消 ----

// Signal 在事务内校验并应用人工选择。点击与超时在同一行锁下竞争，只有先到的生效。
func (e *Engine) Signal(ctx context.Context, cmd SignalCmd) (SignalResult, error) {
	var result SignalResult
	var jobs []Job
	err := e.store.Tx(ctx, func(t *tx) error {
		s, err := e.load(ctx, t, cmd.RunID)
		if err != nil {
			return err
		}
		reject := func(reason string) error {
			result = SignalResult{Reason: reason}
			return nil
		}
		r := s.rows[cmd.NodeID]
		n := s.node(cmd.NodeID)
		switch {
		case s.run.Status != RunRunning:
			return reject("流程已结束")
		case r == nil || n.Type != dsl.NodeHumanChoice:
			return reject("节点不存在或不是人工选择节点")
		case NodeStatus(r.Status) != NodeWaiting:
			return reject("该节点已处理")
		case int(r.Iteration) != cmd.Iteration:
			return reject("卡片已过期，请查看最新卡片")
		}
		cfg, err := dsl.DecodeConfig(n.Type, n.Config)
		if err != nil {
			return err
		}
		var opt *dsl.Option
		for i, o := range cfg.(*dsl.HumanChoiceConfig).Options {
			if o.Key == cmd.Option {
				opt = &cfg.(*dsl.HumanChoiceConfig).Options[i]
			}
		}
		if opt == nil {
			return reject(fmt.Sprintf("未知选项 %q", cmd.Option))
		}
		if opt.Snooze != nil {
			return reject("暂不支持推迟，将在提醒功能上线后开放")
		}
		for _, f := range opt.RequiredFields {
			if strings.TrimSpace(cmd.Form[f]) == "" {
				return reject(fmt.Sprintf("请填写 %s", f))
			}
		}
		if err := t.jobs.CancelPending(t.ctx, uint64(s.run.ID), cmd.NodeID, r.Iteration, []string{string(JobTimeout), string(JobWake)}); err != nil {
			return err
		}
		if err := s.event(cmd.NodeID, "signal", cmd.Actor, map[string]any{"option": cmd.Option, "form": cmd.Form}); err != nil {
			return err
		}
		prev, _ := expr.Decode([]byte(nullOr(r.Output)))
		out, _ := json.Marshal(map[string]any{"option": cmd.Option, "form": cmd.Form, "actor": cmd.Actor, "assignment": prev})
		if err := s.complete(cmd.NodeID, cmd.Option, out); err != nil {
			return err
		}
		if err := s.commit(); err != nil {
			return err
		}
		result, jobs = SignalResult{Accepted: true}, s.jobs
		return nil
	})
	if err == nil {
		e.drive(ctx, jobs)
	}
	return result, err
}

// Cancel 取消 run：撤销尚未执行的 job，任务置为 solved（auto_closed）。已结束的 run 不受影响。
func (e *Engine) Cancel(ctx context.Context, runID int64, actor, reason string) error {
	var jobs []Job
	err := e.store.Tx(ctx, func(t *tx) error {
		s, err := e.load(ctx, t, runID)
		if err != nil {
			return err
		}
		if s.run.Status.Terminal() {
			return nil
		}
		s.run.Status = RunCancelled
		if err := t.jobs.CancelRun(t.ctx, uint64(runID)); err != nil {
			return err
		}
		s.sync = SyncSolved
		if err := s.event("", "run_cancelled", actor, map[string]any{"reason": reason}); err != nil {
			return err
		}
		if err := s.commit(); err != nil {
			return err
		}
		jobs = s.jobs
		return nil
	})
	if err == nil {
		e.drive(ctx, jobs) // 执行任务同步
	}
	return err
}

// GetRun 返回 run 及其节点状态。
func (e *Engine) GetRun(ctx context.Context, runID int64) (*RunView, error) {
	return e.store.GetRun(ctx, runID)
}

// ---- 求值环境 ----

// buildEnv 构造节点可见的表达式环境：payload、task、nodes.<id>.output / port / status、loop.iteration。
func buildEnv(run Run, rows map[string]*model.WfNodeRun, iteration int) expr.Env {
	payload, _ := expr.Decode(run.Vars.Payload)
	nodes := map[string]any{}
	for id, r := range rows {
		out, _ := expr.Decode([]byte(nullOr(r.Output)))
		nodes[id] = map[string]any{"output": out, "port": r.Port, "status": r.Status}
	}
	return expr.Env{
		expr.RootPayload: payload,
		expr.RootTask: map[string]any{
			"id": run.Vars.Task.ID, "type": run.Vars.Task.Type, "priority": run.Vars.Task.Priority,
			"assignee": run.Vars.Task.Assignee, "group": run.Vars.Task.Group,
		},
		expr.RootNodes: nodes,
		expr.RootLoop:  map[string]any{"iteration": iteration},
	}
}
