// Package dryrun 实现任务类型的试运行：复用同一个编译器、推进器与节点执行器，只替换会产生副作用的部分，
// 因此试出来的路径就是上线后的真实路径（设计见 docs/workflow-engine.md「试运行」）。
//
//   - 存储：独立的试运行库（结构同 deploy/sql），worker 与 ticker 不连接它，试运行的 job 不会被当作正式 job 执行；
//     每次试运行前后清空库中的运行数据。
//   - 时钟：每个样本一个引擎实例与可快进的时钟，逐个跳到下一个到期的 job，定时器与超时即时触发。
//     同一时刻只运行一个试运行（进程内互斥 + 试运行库 wf_plan 中的锁行），库里只有这一个 run，引擎的 ProcessDue 就只推进它。
//   - 动作：发卡片、邮件、事件与任务同步只记录不执行；只调用平台内置的只读 skill，其他 skill 记录为未执行；
//     分配服务与 LLM 照常调用。
//   - 人工节点：按预设选择自动提交；没有预设时，配置了超时的走超时，否则停在等待。
package dryrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/model"
)

// 防失控的上限。
const (
	DefaultMaxSteps = 200
	DefaultHorizon  = 90 * 24 * time.Hour
)

// lockKey 是试运行库 wf_plan 中锁行的 plan_id：多个实例共用一个试运行库时，靠它串行化。
// 锁行不放在要清空的表里：READ-COMMITTED 下删除时扫描到被锁的行也会等待。
// 试运行的定义来自 staticPlans，不读 wf_plan。
const lockKey = "dryrun:lock"

// Runner 执行试运行。
type Runner struct {
	conn     sqlx.SqlConn // 试运行库
	base     engine.Invoker
	readOnly func(skill string) bool
	execs    engine.Executors
	mu       sync.Mutex

	MaxSteps int           // 每个样本最多推进的步数
	Horizon  time.Duration // 虚拟时间上限，从样本开始算起
}

// New 创建 Runner。conn 必须指向专用的试运行库，不能是平台库：每次试运行都会清空其中的运行数据。
// base 是真实的动作运行时，只用于只读 skill、分配服务与 LLM；readOnly 判断 skill 是否可以真实调用。
func New(conn sqlx.SqlConn, base engine.Invoker, execs engine.Executors, readOnly func(string) bool) *Runner {
	return &Runner{conn: conn, base: base, execs: execs, readOnly: readOnly, MaxSteps: DefaultMaxSteps, Horizon: DefaultHorizon}
}

// Input 是一次试运行的输入。
type Input struct {
	Def     *engine.Definition
	Samples []json.RawMessage // payload 样本
	Choices map[string]string // 人工节点预设选择：node_id -> option
	Now     time.Time         // 虚拟时钟的起点
}

// Report 是试运行报告，每个样本一份。
type Report struct {
	Cases []Case
}

// Case 是一个样本的试运行结果。
type Case struct {
	Index     int
	Path      []Step
	Assignee  string // 最后确定的处理人；组队列为 group:<组名>
	Sends     []Send
	Timeline  []TimePoint
	EndStatus string // 结束时 run 的状态；停在人工节点上为 waiting
	Issues    []Issue
}

// Step 是走过的一个节点。
type Step struct {
	NodeID    string
	Iteration int
	Port      string
	Status    string
}

// Send 是一次“会发出去”的外部动作。
type Send struct {
	NodeID   string
	Kind     string
	Target   string
	Summary  string
	Rendered json.RawMessage
}

// TimePoint 是虚拟时钟上的一个到期点，Offset 相对样本开始。
type TimePoint struct {
	NodeID string
	Kind   string // timer / timeout
	Offset time.Duration
}

// Issue 是试运行中发现的问题。
type Issue struct {
	Code    string
	NodeID  string
	Message string
}

// Run 执行试运行。同一时刻只有一个试运行在执行。
func (r *Runner) Run(ctx context.Context, in Input) (*Report, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.conn.ExecCtx(ctx, "insert ignore into `wf_plan` (`plan_id`, `version`, `dsl`, `compiled`) values (?, 0, '{}', '{}')", lockKey); err != nil {
		return nil, fmt.Errorf("dryrun: lock row: %w", err)
	}
	var report *Report
	err := r.conn.TransactCtx(ctx, func(ctx context.Context, s sqlx.Session) error {
		var id uint64
		if err := s.QueryRowCtx(ctx, &id, "select `id` from `wf_plan` where `plan_id` = ? and `version` = 0 for update", lockKey); err != nil {
			return fmt.Errorf("dryrun: lock: %w", err)
		}
		if err := r.clean(ctx); err != nil {
			return err
		}
		defer func() { _ = r.clean(context.WithoutCancel(ctx)) }()
		report = &Report{}
		for i, sample := range in.Samples {
			c, err := r.runCase(ctx, in, i, sample)
			if err != nil {
				return err
			}
			report.Cases = append(report.Cases, *c)
		}
		return nil
	})
	return report, err
}

// clean 清空试运行库中的运行数据。上次试运行异常退出留下的数据也在这里清掉。
func (r *Runner) clean(ctx context.Context) error {
	for _, q := range []string{
		"delete from `wf_job`", "delete from `wf_node_run`", "delete from `wf_node_run_history`",
		"delete from `wf_event`", "delete from `wf_loop_counter`", "delete from `wf_run`",
	} {
		if _, err := r.conn.ExecCtx(ctx, q); err != nil {
			return fmt.Errorf("dryrun: clean: %w", err)
		}
	}
	return nil
}

func (r *Runner) runCase(ctx context.Context, in Input, index int, sample json.RawMessage) (*Case, error) {
	c := &Case{Index: index}
	var probe map[string]any
	if err := json.Unmarshal(sample, &probe); err != nil {
		c.Issues = append(c.Issues, Issue{Code: "bad_sample", Message: fmt.Sprintf("样本不是 JSON 对象：%v", err)})
		c.EndStatus = "invalid"
		return c, nil
	}
	clock := engine.NewFakeClock(in.Now)
	rec := &recorder{base: r.base, readOnly: r.readOnly}
	eng := engine.New(engine.NewStore(r.conn), staticPlans{in.Def}, r.execs, rec, clock, engine.Options{Owner: "dryrun"})
	run, _, err := eng.StartRun(ctx, engine.StartRunCmd{
		PlanKey: in.Def.Key, PlanVersion: in.Def.Version, BizKey: fmt.Sprintf("dryrun:%d", index), DryRun: true,
		Vars: engine.Vars{Payload: sample, Task: engine.TaskRef{Type: in.Def.Key, Priority: in.Def.Spec.Priority()}},
	})
	if err != nil {
		return nil, fmt.Errorf("dryrun: start: %w", err)
	}
	start := clock.Now()
	jobs := model.NewWfJobModel(r.conn)
	signaled := map[string]bool{}
	seen := map[uint64]bool{}
	finished := false
	for step := 0; step < r.MaxSteps && !finished; step++ {
		snap, err := eng.Snapshot(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		if snap.Run.Status != engine.RunRunning {
			break
		}
		if n := waitingHuman(snap); n != nil {
			key := fmt.Sprintf("%s:%d", n.NodeID, n.Iteration)
			if opt, ok := in.Choices[n.NodeID]; ok && !signaled[key] {
				signaled[key] = true
				// 不带 actor：报告里的处理人保持为节点分配的人或组，而不是“试运行”。
				res, err := eng.Signal(ctx, engine.SignalCmd{RunID: run.ID, NodeID: n.NodeID, Iteration: n.Iteration, Option: opt})
				if err != nil {
					return nil, err
				}
				if !res.Accepted {
					c.Issues = append(c.Issues, Issue{Code: "choice_rejected", NodeID: n.NodeID, Message: fmt.Sprintf("预设选择 %q 被拒绝：%s", opt, res.Reason)})
				}
				continue
			}
		}
		pending, err := jobs.FindPendingByRun(ctx, uint64(run.ID))
		if err != nil {
			return nil, err
		}
		var next *model.WfJob
		for _, j := range pending {
			if next == nil || j.DueAt.Before(next.DueAt) {
				next = j
			}
		}
		switch {
		case next == nil:
			finished = true // 没有待处理的 job：停在没有预设选择、也没有超时的人工节点上
			continue
		case next.DueAt.After(start.Add(r.Horizon)):
			c.Issues = append(c.Issues, Issue{Code: "horizon", NodeID: next.NodeId,
				Message: fmt.Sprintf("下一个到期点超出虚拟时间上限 %s", r.Horizon)})
			finished = true
			continue
		}
		if d := next.DueAt.Sub(clock.Now()); d > 0 {
			clock.Advance(d)
		}
		for _, j := range pending {
			kind := engine.JobKind(j.Kind)
			if seen[j.Id] || j.DueAt.After(clock.Now()) || (kind != engine.JobTimer && kind != engine.JobTimeout && kind != engine.JobWake) {
				continue
			}
			seen[j.Id] = true
			c.Timeline = append(c.Timeline, TimePoint{NodeID: j.NodeId, Kind: j.Kind, Offset: j.DueAt.Sub(start)})
		}
		if _, err := eng.ProcessDue(ctx, engine.TickOptions{}); err != nil {
			return nil, err
		}
		if step == r.MaxSteps-1 {
			c.Issues = append(c.Issues, Issue{Code: "max_steps", Message: fmt.Sprintf("超过最大推进步数 %d，可能存在未收敛的流程", r.MaxSteps)})
		}
	}
	snap, err := eng.Snapshot(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	for _, n := range snap.Nodes {
		switch n.Status {
		case engine.NodePending, engine.NodeReady, engine.NodeSkipped:
			continue
		}
		c.Path = append(c.Path, Step{NodeID: n.NodeID, Iteration: n.Iteration, Port: n.Port, Status: string(n.Status)})
		if n.Status == engine.NodeFailed {
			c.Issues = append(c.Issues, Issue{Code: "node_failed", NodeID: n.NodeID, Message: "节点执行失败：" + string(n.Output)})
		}
	}
	c.EndStatus = string(snap.Run.Status)
	if snap.Run.Status == engine.RunRunning && waitingHuman(snap) != nil {
		c.EndStatus = "waiting"
	}
	if t := snap.Run.Vars.Task; t.Assignee != "" {
		c.Assignee = t.Assignee
	} else if t.Group != "" {
		c.Assignee = "group:" + t.Group
	}
	c.Sends = rec.sends
	return c, nil
}

func waitingHuman(s *engine.Snapshot) *engine.NodeRun {
	for i := range s.Nodes {
		n := &s.Nodes[i]
		if node, _ := s.Def.Node(n.NodeID); node.Type == dsl.NodeHumanChoice && n.Status == engine.NodeWaiting {
			return n
		}
	}
	return nil
}

// staticPlans 只提供被试运行的那一个定义（可以是尚未发布的草稿）。
type staticPlans struct{ def *engine.Definition }

func (p staticPlans) Load(_ context.Context, key string, version int) (*engine.Definition, error) {
	if key != p.def.Key || version != p.def.Version {
		return nil, fmt.Errorf("dryrun: unexpected plan %s@%d", key, version)
	}
	return p.def, nil
}

// recorder 拦截有副作用的动作，记录“发给谁、发什么”。引擎按 DriveParallelism 并发执行节点，记录需要加锁。
type recorder struct {
	base     engine.Invoker
	readOnly func(string) bool
	mu       sync.Mutex
	sends    []Send
}

func (r *recorder) InvokeAction(ctx context.Context, req action.Request) (action.Result, error) {
	node := nodeOf(req.IdemKey)
	switch req.Kind {
	case action.KindLarkCard:
		var in action.LarkCardInput
		if err := json.Unmarshal(req.Input, &in); err != nil {
			return action.Result{}, err
		}
		in.Card.Callback = nil
		rendered, _ := json.Marshal(in.Card)
		n := r.record(Send{NodeID: node, Kind: string(req.Kind), Target: target(in.To), Summary: in.Card.Title, Rendered: rendered})
		return result(action.LarkCardOutput{MessageID: fmt.Sprintf("dryrun-%d", n)})
	case action.KindSendEmail:
		var in action.EmailInput
		if err := json.Unmarshal(req.Input, &in); err != nil {
			return action.Result{}, err
		}
		var rendered json.RawMessage
		if in.Card != nil {
			rendered, _ = json.Marshal(in.Card)
		}
		r.record(Send{NodeID: node, Kind: string(req.Kind), Target: strings.Join(in.To, ","), Summary: in.Subject, Rendered: rendered})
		return result(map[string]any{})
	case action.KindEmitEvent:
		var in action.EventInput
		if err := json.Unmarshal(req.Input, &in); err != nil {
			return action.Result{}, err
		}
		r.record(Send{NodeID: node, Kind: string(req.Kind), Target: in.EventType, Summary: in.EventType})
		return result(map[string]any{})
	case action.KindSyncTask:
		return result(map[string]any{}) // 试运行没有任务，引擎也不会发出同步
	case action.KindSkill:
		var cfg action.SkillConfig
		_ = json.Unmarshal(req.Config, &cfg)
		if r.readOnly != nil && r.readOnly(cfg.Skill) {
			return r.base.InvokeAction(ctx, req)
		}
		r.record(Send{NodeID: node, Kind: "skill_call", Target: cfg.Skill, Summary: "未执行：不是平台内置的只读 skill"})
		return result(map[string]any{})
	default: // 分配服务、LLM：没有外部副作用，照常调用
		return r.base.InvokeAction(ctx, req)
	}
}

func (r *recorder) record(s Send) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sends = append(r.sends, s)
	return len(r.sends)
}

func result(v any) (action.Result, error) {
	raw, err := json.Marshal(v)
	return action.Result{Output: raw}, err
}

func target(to action.AssignOutput) string {
	if to.Assignee != "" {
		return to.Assignee
	}
	if to.Group != "" {
		return "group:" + to.Group
	}
	return ""
}

// nodeOf 从幂等键 run_id:node_id:iteration[:...] 中取出节点 id。
func nodeOf(idemKey string) string {
	parts := strings.Split(idemKey, ":")
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}
