package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
)

// 重试策略：effect 在外部系统（如 Lark）短暂不可用时多等一会儿，约 30 分钟后放弃；
// failPolicy 用于处理 job 时出错（加载定义、提交事务出错）后的退避。
var (
	effectPolicy = dsl.RetryPolicy{InitialInterval: dsl.Duration(2 * time.Second), BackoffCoefficient: 2, MaxInterval: dsl.Duration(5 * time.Minute), MaxAttempts: 10}
	failPolicy   = dsl.RetryPolicy{InitialInterval: dsl.Duration(2 * time.Second), BackoffCoefficient: 2, MaxInterval: dsl.Duration(5 * time.Minute)}
)

// process 处理一个 job，返回处理中新插入的 job。worked 为 false 表示 job 已被别人处理或持有，本次什么都没做。
// 认领成功后 j.Attempt 更新为这次认领的凭证。
func (e *Engine) process(ctx context.Context, j *Job) (next []Job, worked bool, err error) {
	switch j.Kind {
	case JobExecute:
		return e.execute(ctx, j)
	case JobTimer, JobTimeout:
		return e.fire(ctx, j)
	case JobEffect:
		return e.effect(ctx, j)
	default:
		return nil, true, fmt.Errorf("unsupported job kind %q", j.Kind)
	}
}

// detached 返回一个不随调用方取消的 ctx，用于提交已经拿到的执行结果：结果一旦产生就提交，
// 进程退出时也不丢弃，避免同一次执行被租约到期后再做一遍。
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), commitTimeout)
}

// ---- 执行节点 ----

// execution 是认领成功后在事务外执行节点所需的一切。
type execution struct {
	ec      ExecContext
	token   int
	timeout time.Duration
}

// claimed 是认领事务的结果：x 为 nil 时不执行；worked 为 false 表示 job 不可认领。
type claimed[X any] struct {
	x      *X
	worked bool
}

// execute 分三步：认领（短事务）→ 事务外执行 → 提交结果（短事务）。
func (e *Engine) execute(ctx context.Context, j *Job) ([]Job, bool, error) {
	c, jobs, err := update(ctx, e, j.RunID, j.ID, e.startExecution)
	if err != nil || c.x == nil {
		return jobs, c.worked, err
	}
	x := c.x
	j.Attempt = x.token
	res := e.invoke(ctx, x)
	interrupted := res.Err != nil && ctx.Err() != nil
	actx, cancel := detached(ctx)
	defer cancel()
	_, jobs, err = update(actx, e, j.RunID, j.ID, func(s *runState) (struct{}, error) {
		return struct{}{}, e.finishExecution(s, x, res, interrupted)
	})
	return jobs, true, err
}

// startExecution 在 run 锁内认领 execute job：确认节点仍待执行，把节点置为 running、执行次数加 1，
// 租约设为节点超时之后。认领与节点状态在同一事务里提交，不存在“认领了但还没开始”的中间状态。
func (e *Engine) startExecution(s *runState) (claimed[execution], error) {
	j := s.job
	if !s.claimable() {
		return claimed[execution]{}, nil
	}
	done := claimed[execution]{worked: true}
	id := j.NodeId
	r := s.nodes[id]
	if s.run.Status != RunRunning || r == nil || r.Iteration != j.Iteration ||
		(NodeStatus(r.Status) != NodeReady && NodeStatus(r.Status) != NodeRunning) {
		s.finishJob(JobCancelled, "stale")
		return done, nil
	}
	n, cfg := s.def.node(id)
	if s.leaseExpired() {
		// 上一次执行的租约到期仍没有结果（进程崩溃或卡住），按一次超时失败计入重试次数。
		s.event(id, "node_lease_expired", "", map[string]any{"attempt": r.Attempt, "owner": j.Owner})
		if int(r.Attempt) >= RetryPolicyFor(n).MaxAttempts {
			err := &action.Error{Code: action.CodeTimeout, Err: fmt.Errorf("node %s: no result after %d attempts (lease expired)", id, r.Attempt)}
			e.fail(s, NodeResult{Err: err})
			return done, nil
		}
	}
	if s.run.ExecCount >= e.opts.MaxExecCount {
		s.finishJob(JobCancelled, "max exec count")
		s.needsIntervention(id, "exceeded max exec count")
		return done, nil
	}
	timeout := ExecTimeout(cfg)
	token := s.claim(e.opts.Owner, s.now.Add(timeout+leaseMargin))
	s.run.ExecCount++
	s.setNode(id, NodeRunning, func(r *model.WfNodeRun) {
		r.Attempt++
		r.StartedAt = s.at()
	})
	s.event(id, "node_started", "", map[string]any{"attempt": r.Attempt})
	ec := ExecContext{
		Run: s.run, Def: s.def, Node: n, Config: cfg, Iteration: int(r.Iteration), Attempt: int(r.Attempt),
		IdemKey: s.actionKey(id), Env: buildEnv(s.run, s.nodes, int(r.Iteration)), Now: s.now, Actions: e.actions,
	}
	if b, ok := s.def.Plan.Bindings[id]; ok {
		ec.Binding = &b
	}
	done.x = &execution{ec: ec, token: token, timeout: timeout}
	return done, nil
}

// invoke 在事务外执行节点，不持有任何数据库锁；超时后取消调用，按可重试的 timeout 错误处理。
func (e *Engine) invoke(ctx context.Context, x *execution) NodeResult {
	ex, ok := e.execs.Get(x.ec.Node.Type)
	if !ok {
		return NodeResult{Err: fmt.Errorf("no executor for node type %q", x.ec.Node.Type), Permanent: true}
	}
	ectx, cancel := context.WithTimeout(ctx, x.timeout)
	defer cancel()
	res := ex.Execute(ectx, x.ec)
	if res.Err != nil && errors.Is(ectx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		res.Err = &action.Error{Code: action.CodeTimeout, Err: fmt.Errorf("node %s timed out after %s: %w", x.ec.Node.ID, x.timeout, res.Err)}
		res.Permanent = false
	}
	return res
}

// finishExecution 在 run 锁内提交执行结果。凭证失效（job 已被重新认领、或已被撤销）时只丢弃结果，什么都不改。
func (e *Engine) finishExecution(s *runState, x *execution, res NodeResult, interrupted bool) error {
	if !s.holds(x.token) {
		return nil
	}
	id := s.job.NodeId
	r := s.nodes[id]
	if s.run.Status != RunRunning || r == nil || NodeStatus(r.Status) != NodeRunning || r.Iteration != s.job.Iteration {
		s.finishJob(JobCancelled, "fenced") // run 已结束或节点已被退回，结果丢弃
		return nil
	}
	switch {
	case interrupted:
		// 进程退出打断了执行：放回队列，由其他实例的 tick 立即接手，不计入重试次数。
		s.releaseJob(s.now, "interrupted")
		s.setNode(id, NodeReady, func(r *model.WfNodeRun) { r.Attempt-- })
		return nil
	case res.Err != nil:
		e.fail(s, res)
		return nil
	}
	s.finishJob(JobDone, "")
	for _, ef := range res.Effects {
		if err := s.addEffect(id, ef); err != nil {
			return err
		}
	}
	if res.Wait != nil {
		s.wait(id, res)
		return nil
	}
	s.assign(id, res.Assignment)
	s.complete(id, res.Port, res.Output)
	return nil
}

// fail 按重试策略处理一次失败的执行：还有次数则退避后重试；否则走 error 端口，没有 error 边时 run 进入人工干预。
func (e *Engine) fail(s *runState, res NodeResult) {
	id := s.job.NodeId
	r := s.nodes[id]
	n, _ := s.def.node(id)
	policy := RetryPolicyFor(n)
	msg, code := res.Err.Error(), action.ErrorCode(res.Err)
	if Retryable(policy, res.Err, res.Permanent) && int(r.Attempt) < policy.MaxAttempts {
		due := s.now.Add(backoff(policy, int(r.Attempt)))
		s.releaseJob(due, msg)
		s.setNode(id, NodeReady, nil)
		s.event(id, "node_retry", "", map[string]any{"attempt": r.Attempt, "error": msg, "code": code, "next_at": due})
		return
	}
	s.finishJob(JobDone, msg)
	out, _ := json.Marshal(map[string]string{"error": msg, "code": code})
	if s.hasEdge(id, dsl.PortError) {
		s.complete(id, dsl.PortError, out)
		return
	}
	s.setNode(id, NodeFailed, func(r *model.WfNodeRun) {
		r.FinishedAt = s.at()
		r.Output = nullJSON(out)
	})
	s.event(id, "node_failed", "", map[string]any{"error": msg, "code": code, "attempt": r.Attempt})
	s.needsIntervention(id, msg)
}

// ---- 定时 ----

// fire 处理到期的 timer / timeout。没有外部 IO，一个事务完成，不需要认领。
func (e *Engine) fire(ctx context.Context, j *Job) ([]Job, bool, error) {
	worked, jobs, err := update(ctx, e, j.RunID, j.ID, func(s *runState) (bool, error) {
		job := s.job
		if job.Status != string(JobPending) || job.DueAt.After(s.now) {
			return false, nil
		}
		s.finishJob(JobDone, "")
		if !s.waiting(job.NodeId, job.Iteration) {
			return true, nil // 节点已被点击或已退回，定时作废
		}
		port := dsl.PortNext
		if JobKind(job.Kind) == JobTimeout {
			port = dsl.PortTimeout
		}
		s.cancelJobs(job.NodeId, JobTimer, JobTimeout)
		s.complete(job.NodeId, port, json.RawMessage(nullOr(s.nodes[job.NodeId].Output)))
		return true, nil
	})
	return jobs, worked, err
}

// ---- effect ----

// effect 执行一个 effect job：认领（短事务）→ 调用外部动作 → 提交结果（短事务）。
// 要求 WhileWaiting 而节点已不在等待（已被点击、超时或 run 已结束）时撤销。
// 失败按 effectPolicy 退避重试；用尽后 job 置为 failed，run 仍在运行时转为人工干预——处理人没收到卡片，流程不能静默等待。
func (e *Engine) effect(ctx context.Context, j *Job) ([]Job, bool, error) {
	type claim struct {
		p     EffectPayload
		token int
	}
	c, _, err := update(ctx, e, j.RunID, j.ID, func(s *runState) (claimed[claim], error) {
		job := s.job
		if !s.claimable() {
			return claimed[claim]{}, nil
		}
		done := claimed[claim]{worked: true}
		var p EffectPayload
		if err := json.Unmarshal([]byte(nullOr(job.Payload)), &p); err != nil {
			return done, fmt.Errorf("effect payload: %w", err)
		}
		if p.WhileWaiting && !s.waiting(job.NodeId, job.Iteration) {
			s.finishJob(JobCancelled, "node no longer waiting")
			return done, nil
		}
		if int(job.Attempt) >= effectPolicy.MaxAttempts { // 只在最后一次的租约到期时发生
			s.effectFailed(p, fmt.Sprintf("no result after %d attempts (lease expired)", job.Attempt))
			return done, nil
		}
		done.x = &claim{p: p, token: s.claim(e.opts.Owner, s.now.Add(DefaultActionTimeout+leaseMargin))}
		return done, nil
	})
	if err != nil || c.x == nil {
		return nil, c.worked, err
	}
	p, token := c.x.p, c.x.token
	j.Attempt = token

	ectx, cancel := context.WithTimeout(ctx, DefaultActionTimeout)
	res, callErr := e.actions.InvokeAction(ectx, action.Request{Kind: action.Kind(p.Kind), Config: p.Config, Input: p.Input, IdemKey: p.IdemKey, DryRun: p.DryRun})
	cancel()
	interrupted := callErr != nil && ctx.Err() != nil

	actx, acancel := detached(ctx)
	defer acancel()
	_, jobs, err := update(actx, e, j.RunID, j.ID, func(s *runState) (struct{}, error) {
		if !s.holds(token) {
			return struct{}{}, nil
		}
		switch {
		case callErr == nil:
			s.finishJob(JobDone, "")
			s.event(s.job.NodeId, "effect_done", "", map[string]any{"kind": p.Kind, "output": json.RawMessage(nullIfEmpty(res.Output))})
		case interrupted:
			s.releaseJob(s.now, "interrupted")
		case Retryable(effectPolicy, callErr, false) && token < effectPolicy.MaxAttempts:
			s.releaseJob(s.now.Add(backoff(effectPolicy, token)), callErr.Error())
		default:
			s.effectFailed(p, callErr.Error())
		}
		return struct{}{}, nil
	})
	return jobs, true, err
}

func (s *runState) effectFailed(p EffectPayload, msg string) {
	id := s.job.NodeId
	s.finishJob(JobFailed, msg)
	s.event(id, "effect_failed", "", map[string]any{"kind": p.Kind, "error": msg, "attempt": s.job.Attempt})
	if s.run.Status == RunRunning {
		s.needsIntervention(id, "effect "+p.Kind+" failed: "+msg)
	}
}

func nullIfEmpty(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}

// ---- 处理出错 ----

// failed 处理一次出错的处理（加载定义、提交事务出错等，事务已回滚）：job 退避后重试并计数，
// 连续出错达到 MaxFailCount 时隔离（置为 failed），run 仍在运行时转为人工干预。
//
// 这里不经过 update：定义加载失败、变量损坏这类问题正是出错的原因。租约不动——若出错时 job 已被认领，
// 它在租约到期后才会被重新执行，并按租约到期计入一次执行。
func (e *Engine) failed(ctx context.Context, j Job, cause error) {
	_, err := transact(ctx, e.store, func(t *tx) (struct{}, error) {
		run, err := t.runs.FindOneForUpdate(t.ctx, uint64(j.RunID))
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return struct{}{}, err
		}
		job, err := t.jobs.FindOne(t.ctx, uint64(j.ID))
		if err != nil {
			return struct{}{}, err
		}
		if job.Status != string(JobPending) || int(job.Attempt) != j.Attempt {
			return struct{}{}, nil // 已被别人处理
		}
		job.FailCount++
		if int(job.FailCount) < e.opts.MaxFailCount {
			job.DueAt, job.LastError = e.clock.Now().Add(backoff(failPolicy, int(job.FailCount))), truncate(cause.Error())
			return struct{}{}, t.jobs.Update(t.ctx, job)
		}
		reason := fmt.Sprintf("failed %d times: %v", job.FailCount, cause)
		job.Status, job.LastError = string(JobFailed), truncate("quarantined: "+reason)
		if err := t.jobs.Update(t.ctx, job); err != nil {
			return struct{}{}, err
		}
		if run != nil && run.Status == string(RunRunning) {
			run.Status, run.Version = string(RunNeedsIntervention), run.Version+1
			if err := t.runs.Update(t.ctx, run); err != nil {
				return struct{}{}, err
			}
		}
		payload, _ := json.Marshal(map[string]any{"job_id": job.Id, "kind": job.Kind, "attempt": job.Attempt, "fail_count": job.FailCount, "reason": reason})
		_, err = t.evts.Insert(t.ctx, &model.WfEvent{RunId: job.RunId, NodeId: job.NodeId, Iteration: job.Iteration, Type: "job_quarantined", Actor: "system",
			Payload: nullJSON(payload)})
		return struct{}{}, err
	})
	if err != nil {
		logx.WithContext(ctx).Errorf("engine: record failure of job %d: %v", j.ID, err)
	}
}

// ---- 重试策略 ----

// Retryable 报告失败是否可以重试：执行器标为 Permanent 的不重试，错误码在 non_retryable_errors 中的不重试。
func Retryable(p dsl.RetryPolicy, err error, permanent bool) bool {
	if permanent {
		return false
	}
	code := action.ErrorCode(err)
	for _, c := range p.NonRetryableErrors {
		if c != "" && c == code {
			return false
		}
	}
	return true
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
		p.NonRetryableErrors = r.NonRetryableErrors
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
