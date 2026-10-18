package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

// update 是修改流程状态的唯一入口：在一个事务内锁住 run 行、加载状态，由 fn 修改内存中的状态，
// 再由 flush 一次写回。jobID 非 0 时一并读取该 job，供 fn 认领或结束它。
//
// job 的状态同样只在这里修改，所以锁住 run 行后读到的 job 就是最新的，不需要额外的行锁或条件更新。
// 只有提交成功才返回 fn 的结果与新插入的 job，由调用方在事务外派发。
func update[T any](ctx context.Context, e *Engine, runID, jobID int64, fn func(s *runState) (T, error)) (T, []Job, error) {
	type result struct {
		v    T
		jobs []Job
	}
	r, err := transact(ctx, e.store, func(t *tx) (result, error) {
		s, err := e.lock(t, runID, jobID)
		if err != nil {
			return result{}, err
		}
		v, err := fn(s)
		if err != nil {
			return result{}, err
		}
		jobs, err := s.flush()
		return result{v: v, jobs: jobs}, err
	})
	return r.v, r.jobs, err
}

// runState 是一次推进在 run 锁内看到的状态，以及它造成的全部变化。
// 状态转换只修改内存，flush 按固定顺序写回；业务逻辑里没有 SQL。
type runState struct {
	t     *tx
	run   Run
	def   *Definition
	nodes map[string]*model.WfNodeRun
	job   *model.WfJob // 正在处理的 job；Signal、Cancel 时为 nil
	now   time.Time

	changed  bool            // run 或节点有变化：flush 时版本加 1 并写回 run
	dirty    map[string]bool // 有变化的节点
	jobDirty bool
	cancels  []cancelReq
	inserts  []*model.WfJob
	events   []*model.WfEvent

	sync       string // 非空时 flush 插入一个任务同步，目标状态见 Sync*
	reassigned bool   // 本次推进明确分配了处理人（节点确定处理人，或有人做了决定）
	current    string // 任务当前所处的节点
}

// cancelReq 撤销某节点当前轮（nodeID 为空时为整个 run）尚未执行的指定类型 job。
type cancelReq struct {
	nodeID string
	kinds  []JobKind
}

func newRunState(t *tx, run Run, def *Definition, now time.Time) *runState {
	return &runState{t: t, run: run, def: def, nodes: map[string]*model.WfNodeRun{}, now: now, dirty: map[string]bool{}}
}

// lock 锁住 run 行，加载定义、全部节点，以及 jobID 对应的 job。
func (e *Engine) lock(t *tx, runID, jobID int64) (*runState, error) {
	r, err := t.runs.FindOneForUpdate(t.ctx, uint64(runID))
	if err != nil {
		return nil, fmt.Errorf("engine: lock run %d: %w", runID, err)
	}
	run, err := runFromRow(r)
	if err != nil {
		return nil, err
	}
	def, err := e.plans.Load(t.ctx, run.PlanKey, run.PlanVersion) // 有进程内缓存，通常不访问数据库
	if err != nil {
		return nil, err
	}
	rows, err := t.nodes.FindByRun(t.ctx, uint64(runID))
	if err != nil {
		return nil, err
	}
	s := newRunState(t, run, def, e.clock.Now())
	for _, n := range rows {
		s.nodes[n.NodeId] = n
	}
	if jobID != 0 {
		if s.job, err = t.jobs.FindOne(t.ctx, uint64(jobID)); err != nil {
			return nil, fmt.Errorf("engine: load job %d: %w", jobID, err)
		}
	}
	return s, nil
}

// flush 把内存中的变化写回当前事务，返回新插入的 job。没有任何变化时不写库。
func (s *runState) flush() ([]Job, error) {
	t := s.t
	if s.changed {
		s.run.Version++ // 只有改变了流程状态的推进才加 1；它同时是任务同步的 seq，保证任务中心按序应用
		if err := s.addSync(); err != nil {
			return nil, err
		}
	}
	for _, id := range s.def.Plan.Order { // 按拓扑序写回，顺序稳定
		r := s.nodes[id]
		switch {
		case r == nil:
		case r.Id == 0:
			if _, err := t.nodes.Insert(t.ctx, r); err != nil {
				return nil, err
			}
		case s.dirty[id]:
			if err := t.nodes.Update(t.ctx, r); err != nil {
				return nil, err
			}
		}
	}
	if err := s.applyCancels(); err != nil {
		return nil, err
	}
	if s.jobDirty {
		if err := t.jobs.Update(t.ctx, s.job); err != nil {
			return nil, err
		}
	}
	var jobs []Job
	for _, j := range s.inserts {
		ok, err := t.jobs.InsertIgnore(t.ctx, j)
		if err != nil {
			return nil, err
		}
		if ok {
			jobs = append(jobs, jobFromRow(j))
		}
	}
	for _, ev := range s.events {
		if _, err := t.evts.Insert(t.ctx, ev); err != nil {
			return nil, err
		}
	}
	if s.changed {
		row, err := runToRow(s.run)
		if err != nil {
			return nil, err
		}
		if err := t.runs.Update(t.ctx, row); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

// applyCancels 撤销 cancelJobs 记下的 job。正在处理的 job 由处理者自己结束，不在这里撤销。
func (s *runState) applyCancels() error {
	if len(s.cancels) == 0 {
		return nil
	}
	rows, err := s.t.jobs.FindPendingByRun(s.t.ctx, uint64(s.run.ID))
	if err != nil {
		return err
	}
	for _, j := range rows {
		if s.job != nil && j.Id == s.job.Id {
			continue
		}
		for _, c := range s.cancels {
			if c.matches(s, j) {
				j.Status = string(JobCancelled)
				if err := s.t.jobs.Update(s.t.ctx, j); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func (c cancelReq) matches(s *runState, j *model.WfJob) bool {
	if !slices.Contains(c.kinds, JobKind(j.Kind)) {
		return false
	}
	return c.nodeID == "" || (j.NodeId == c.nodeID && j.Iteration == s.iteration(c.nodeID))
}

// addSync 把本次推进投影到任务中心：插入一个任务同步 effect，由任务中心按 seq 幂等应用。
func (s *runState) addSync() error {
	if s.sync == "" || s.run.Vars.Task.ID == 0 {
		return nil
	}
	task := s.run.Vars.Task
	p := SyncPayload{
		TaskID: task.ID, Status: s.sync, Reassign: s.reassigned, Assignee: task.Assignee, Group: task.Group,
		CurrentNode: s.current, Seq: s.run.Version,
	}
	if s.sync == SyncSolved {
		p.Resolution = s.resolution()
		if s.run.Status == RunCancelled {
			p.Resolution = "auto_closed"
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.addEffect("", Effect{Kind: action.KindSyncTask, Input: b, Key: fmt.Sprintf(":sync:%d", s.run.Version)})
}

// ---- 读取 ----

func (d *Definition) node(id string) (dsl.Node, any) { return d.nodes[id], d.configs[id] }

func (s *runState) iteration(nodeID string) int64 {
	if r := s.nodes[nodeID]; r != nil {
		return r.Iteration
	}
	return 0
}

// waiting 报告节点在第 iteration 轮是否仍在等待，且 run 仍在运行。
func (s *runState) waiting(nodeID string, iteration int64) bool {
	r := s.nodes[nodeID]
	return s.run.Status == RunRunning && r != nil && r.Iteration == iteration && NodeStatus(r.Status) == NodeWaiting
}

func (s *runState) hasEdge(nodeID, port string) bool {
	for _, e := range s.def.Plan.Out[nodeID] {
		if e.Port == port {
			return true
		}
	}
	return false
}

func (s *runState) at() sql.NullTime { return sql.NullTime{Time: s.now, Valid: true} }

// actionKey 是节点外部动作的幂等键 run_id:node_id:iteration。
func (s *runState) actionKey(nodeID string) string {
	return fmt.Sprintf("%d:%s:%d", s.run.ID, nodeID, s.iteration(nodeID))
}

// ---- 记录变化 ----

func (s *runState) setNode(id string, status NodeStatus, mut func(r *model.WfNodeRun)) {
	r := s.nodes[id]
	r.Status = string(status)
	if mut != nil {
		mut(r)
	}
	s.dirty[id] = true
	s.changed = true
}

func (s *runState) setRunStatus(status RunStatus) {
	s.run.Status = status
	s.changed = true
}

func (s *runState) addJob(nodeID string, kind JobKind, due time.Time, payload json.RawMessage, keySuffix string) {
	iter := s.iteration(nodeID)
	s.inserts = append(s.inserts, &model.WfJob{
		RunId: uint64(s.run.ID), NodeId: nodeID, Iteration: iter, Kind: string(kind), Payload: nullJSON(payload),
		DueAt: due, Status: string(JobPending), IdemKey: IdemKey(s.run.ID, nodeID, int(iter), kind) + keySuffix,
	})
}

// addEffect 把一个外部动作落成 effect job，随本次推进一起提交，提交后再执行。
func (s *runState) addEffect(nodeID string, ef Effect) error {
	b, err := json.Marshal(EffectPayload{
		Kind: string(ef.Kind), Config: ef.Config, Input: ef.Input, IdemKey: s.actionKey(nodeID) + ef.Key,
		DryRun: s.run.DryRun, WhileWaiting: ef.WhileWaiting,
	})
	if err != nil {
		return fmt.Errorf("engine: effect %s of node %s: %w", ef.Kind, nodeID, err)
	}
	s.addJob(nodeID, JobEffect, s.now, b, ef.Key)
	return nil
}

func (s *runState) cancelJobs(nodeID string, kinds ...JobKind) {
	s.cancels = append(s.cancels, cancelReq{nodeID: nodeID, kinds: kinds})
}

func (s *runState) event(nodeID, typ, actor string, payload any) {
	if actor == "" {
		actor = "system"
	}
	ev := &model.WfEvent{RunId: uint64(s.run.ID), NodeId: nodeID, Iteration: s.iteration(nodeID), Type: typ, Actor: actor}
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			b, _ = json.Marshal(map[string]string{"marshal_error": err.Error()})
		}
		ev.Payload = sql.NullString{String: string(b), Valid: true}
	}
	s.events = append(s.events, ev)
}

// ---- 正在处理的 job ----

// claimable 报告 s.job 能否认领：仍是 pending、已到期，且没有有效的租约。
func (s *runState) claimable() bool {
	j := s.job
	if j.Status != string(JobPending) || j.DueAt.After(s.now) {
		return false
	}
	return j.Owner == "" || !j.LockedUntil.Valid || j.LockedUntil.Time.Before(s.now)
}

// leaseExpired 报告 s.job 上一次的认领是否租约到期仍未提交（进程崩溃或卡住）。只在 claimable 为真时有意义。
func (s *runState) leaseExpired() bool { return s.job.Owner != "" }

// claim 认领 s.job，租约到 until；返回这次认领的凭证。之后只有凭证一致的一方能提交结果。
func (s *runState) claim(owner string, until time.Time) int {
	j := s.job
	j.Attempt++
	j.Owner, j.LockedUntil = owner, sql.NullTime{Time: until, Valid: true}
	s.jobDirty = true
	return int(j.Attempt)
}

// holds 报告 s.job 是否仍由凭证 token 持有：重新认领会让 attempt 加 1，结束会改变状态。
func (s *runState) holds(token int) bool {
	return s.job.Status == string(JobPending) && s.job.Owner != "" && int(s.job.Attempt) == token
}

func (s *runState) finishJob(status JobStatus, reason string) {
	j := s.job
	j.Status, j.LastError = string(status), truncate(reason)
	j.Owner, j.LockedUntil = "", sql.NullTime{}
	s.jobDirty = true
}

// releaseJob 释放认领，在 due 之后重新执行。
func (s *runState) releaseJob(due time.Time, reason string) {
	j := s.job
	j.DueAt, j.LastError = due, truncate(reason)
	j.Owner, j.LockedUntil = "", sql.NullTime{}
	s.jobDirty = true
}

func truncate(s string) string {
	if len(s) <= 1024 {
		return s
	}
	return s[:1024]
}

// ---- 状态转换 ----

// assign 记录节点确定的处理人，并在提交时同步任务（pending 或待认领）。
func (s *runState) assign(nodeID string, a *Assignment) {
	if a == nil {
		return
	}
	s.run.Vars.Task.Assignee, s.run.Vars.Task.Group = a.Assignee, a.Group
	s.reassigned = true
	if a.Assignee != "" {
		s.run.Vars.Task.Group = ""
		s.sync = SyncPending
	} else {
		s.sync = SyncUnassigned
	}
	s.current = nodeID
	s.changed = true
}

// complete 把节点标记为成功并传播；所有节点都有结论时结束 run。
func (s *runState) complete(nodeID, port string, output json.RawMessage) {
	s.setNode(nodeID, NodeSucceeded, func(r *model.WfNodeRun) {
		r.Port = port
		r.Output = nullJSON(output)
		r.FinishedAt = s.at()
		r.WaitUntil = sql.NullTime{}
	})
	s.event(nodeID, "node_completed", "", map[string]any{"port": port})
	st := s.states()
	ready, skipped := propagate(s.def.Plan, s.def.modes, st, nodeID)
	for _, id := range skipped {
		s.setNode(id, NodeSkipped, func(r *model.WfNodeRun) { r.FinishedAt = s.at() })
		s.event(id, "node_skipped", "", nil)
	}
	for _, id := range ready {
		s.setNode(id, NodeReady, nil)
		s.addJob(id, JobExecute, s.now, nil, "")
	}
	if allConcluded(s.def.Plan, st) {
		s.finish(RunSucceeded, s.resolution())
	}
}

func (s *runState) states() map[string]*state {
	st := make(map[string]*state, len(s.nodes))
	for id, r := range s.nodes {
		st[id] = &state{Status: NodeStatus(r.Status), Port: r.Port}
	}
	return st
}

// wait 让节点进入等待并插入对应的定时 job。
func (s *runState) wait(nodeID string, res NodeResult) {
	var until *time.Time
	if res.Wait.TimerAt != nil {
		until = res.Wait.TimerAt
		s.addJob(nodeID, JobTimer, *res.Wait.TimerAt, nil, "")
	}
	if res.Wait.TimeoutAt != nil {
		until = res.Wait.TimeoutAt
		s.addJob(nodeID, JobTimeout, *res.Wait.TimeoutAt, nil, "")
	}
	s.setNode(nodeID, NodeWaiting, func(r *model.WfNodeRun) {
		r.Output = nullJSON(res.Output)
		r.WaitUntil = toNullTime(until)
	})
	s.assign(nodeID, res.Assignment)
	s.event(nodeID, "node_waiting", "", map[string]any{"until": until})
}

// finish 结束 run：撤销尚未执行的节点与定时，任务置为 solved。已落库的 effect 照常执行。
func (s *runState) finish(status RunStatus, resolution string) {
	s.setRunStatus(status)
	s.cancelJobs("", graphJobs...)
	s.sync = SyncSolved
	s.event("", "run_"+string(status), "", map[string]any{"resolution": resolution})
}

func (s *runState) needsIntervention(nodeID, reason string) {
	s.setRunStatus(RunNeedsIntervention)
	s.event(nodeID, eventNeedsIntervention, "", map[string]any{"reason": reason})
}

// resolution 取第一个成功结束的 end 节点上配置的结论，否则取最后一个人工节点的选项。
func (s *runState) resolution() string {
	last := ""
	for _, id := range s.def.Plan.Order {
		if NodeStatus(s.nodes[id].Status) != NodeSucceeded {
			continue
		}
		n, cfg := s.def.node(id)
		switch n.Type {
		case dsl.NodeEnd:
			if r := cfg.(*dsl.EndConfig).Resolution; r != "" {
				return r
			}
		case dsl.NodeHumanChoice:
			last = s.nodes[id].Port
		}
	}
	if last == "" {
		return "completed"
	}
	return last
}

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
