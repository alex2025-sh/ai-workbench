package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

// Store 是引擎的持久化层，基于 goctl 生成的模型（internal/model）。
type Store struct {
	conn  sqlx.SqlConn
	runs  model.WfRunModel
	nodes model.WfNodeRunModel
	jobs  model.WfJobModel
	evts  model.WfEventModel
	plans model.WfPlanModel
}

// NewStore 创建 Store。conn 需要 parseTime=true&loc=UTC。
func NewStore(conn sqlx.SqlConn) *Store {
	return &Store{
		conn:  conn,
		runs:  model.NewWfRunModel(conn),
		nodes: model.NewWfNodeRunModel(conn),
		jobs:  model.NewWfJobModel(conn),
		evts:  model.NewWfEventModel(conn),
		plans: model.NewWfPlanModel(conn),
	}
}

// tx 是一次推进事务内可用的模型。
type tx struct {
	ctx   context.Context
	runs  model.WfRunModel
	nodes model.WfNodeRunModel
	jobs  model.WfJobModel
	evts  model.WfEventModel
}

// Tx 在一个事务内执行 fn。MySQL / TiDB 默认即悲观事务，SELECT ... FOR UPDATE 锁行。
func (s *Store) Tx(ctx context.Context, fn func(t *tx) error) error {
	return s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		return fn(&tx{
			ctx:   ctx,
			runs:  s.runs.WithSession(session),
			nodes: s.nodes.WithSession(session),
			jobs:  s.jobs.WithSession(session),
			evts:  s.evts.WithSession(session),
		})
	})
}

// ---- 转换 ----

func runFromRow(r *model.WfRun) (Run, error) {
	run := Run{
		ID: int64(r.Id), PlanKey: r.PlanId, PlanVersion: int(r.PlanVersion), BizKey: r.BizKey,
		Status: RunStatus(r.Status), Version: r.Version, ExecCount: int(r.ExecCount), DryRun: r.DryRun == 1,
	}
	if err := json.Unmarshal([]byte(r.Vars), &run.Vars); err != nil {
		return Run{}, fmt.Errorf("engine: run %d vars: %w", r.Id, err)
	}
	return run, nil
}

func runToRow(run Run, id uint64) (*model.WfRun, error) {
	vars, err := json.Marshal(run.Vars)
	if err != nil {
		return nil, err
	}
	dry := int64(0)
	if run.DryRun {
		dry = 1
	}
	return &model.WfRun{
		Id: id, PlanId: run.PlanKey, PlanVersion: int64(run.PlanVersion), BizKey: run.BizKey, Status: string(run.Status),
		Vars: string(vars), Version: run.Version, ExecCount: int64(run.ExecCount), DryRun: dry,
	}, nil
}

func nodeFromRow(r *model.WfNodeRun) NodeRun {
	n := NodeRun{
		RunID: int64(r.RunId), NodeID: r.NodeId, Iteration: int(r.Iteration), Status: NodeStatus(r.Status),
		Port: r.Port, Attempt: int(r.Attempt),
		WaitUntil: nullTime(r.WaitUntil), StartedAt: nullTime(r.StartedAt), FinishedAt: nullTime(r.FinishedAt),
	}
	if r.Output.Valid {
		n.Output = json.RawMessage(r.Output.String)
	}
	return n
}

func jobFromRow(r *model.WfJob) Job {
	j := Job{
		ID: int64(r.Id), RunID: int64(r.RunId), NodeID: r.NodeId, Iteration: int(r.Iteration), Kind: JobKind(r.Kind),
		DueAt: r.DueAt.UTC(), Status: JobStatus(r.Status), Attempt: int(r.Attempt), Owner: r.Owner,
		LockedUntil: nullTime(r.LockedUntil), IdemKey: r.IdemKey,
	}
	if r.Payload.Valid {
		j.Payload = json.RawMessage(r.Payload.String)
	}
	return j
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

func toNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func nullJSON(b json.RawMessage) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// ---- 事务内操作 ----

// lockRun 锁住 run 行并加载全部节点。
func (t *tx) lockRun(runID int64) (Run, map[string]*model.WfNodeRun, error) {
	r, err := t.runs.FindOneForUpdate(t.ctx, uint64(runID))
	if err != nil {
		return Run{}, nil, fmt.Errorf("engine: lock run %d: %w", runID, err)
	}
	run, err := runFromRow(r)
	if err != nil {
		return Run{}, nil, err
	}
	rows, err := t.nodes.FindByRun(t.ctx, uint64(runID))
	if err != nil {
		return Run{}, nil, err
	}
	nodes := make(map[string]*model.WfNodeRun, len(rows))
	for _, n := range rows {
		nodes[n.NodeId] = n
	}
	return run, nodes, nil
}

func (t *tx) saveRun(run Run) error {
	row, err := runToRow(run, uint64(run.ID))
	if err != nil {
		return err
	}
	return t.runs.Update(t.ctx, row)
}

func (t *tx) saveNode(n *model.WfNodeRun) error { return t.nodes.Update(t.ctx, n) }

// insertJob 按幂等键插入 job；已存在则跳过。返回插入成功的 job（含 ID）。
func (t *tx) insertJob(j Job) (*Job, error) {
	row := &model.WfJob{
		RunId: uint64(j.RunID), NodeId: j.NodeID, Iteration: int64(j.Iteration), Kind: string(j.Kind),
		Payload: nullJSON(j.Payload), DueAt: j.DueAt, Status: string(JobPending), IdemKey: j.IdemKey,
	}
	ok, err := t.jobs.InsertIgnore(t.ctx, row)
	if err != nil || !ok {
		return nil, err
	}
	j.ID = int64(row.Id)
	j.Status = JobPending
	return &j, nil
}

func (t *tx) event(runID int64, nodeID string, iteration int, typ, actor string, payload any) error {
	ev := &model.WfEvent{RunId: uint64(runID), NodeId: nodeID, Iteration: int64(iteration), Type: typ, Actor: actor}
	if actor == "" {
		ev.Actor = "system"
	}
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		ev.Payload = sql.NullString{String: string(b), Valid: true}
	}
	_, err := t.evts.Insert(t.ctx, ev)
	return err
}

// ---- 事务外操作 ----

func (s *Store) findRunByBizKey(ctx context.Context, bizKey string) (*Run, error) {
	r, err := s.runs.FindOneByBizKey(ctx, bizKey)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run, err := runFromRow(r)
	return &run, err
}

// GetRun 返回 run 及其全部节点状态。
func (s *Store) GetRun(ctx context.Context, runID int64) (*RunView, error) {
	r, err := s.runs.FindOne(ctx, uint64(runID))
	if err != nil {
		return nil, err
	}
	run, err := runFromRow(r)
	if err != nil {
		return nil, err
	}
	rows, err := s.nodes.FindByRun(ctx, uint64(runID))
	if err != nil {
		return nil, err
	}
	v := &RunView{Run: run}
	for _, n := range rows {
		v.Nodes = append(v.Nodes, nodeFromRow(n))
	}
	return v, nil
}

// Events 返回一个 run 的事件日志，按写入顺序。
func (s *Store) Events(ctx context.Context, runID int64) ([]model.WfEvent, error) {
	var out []model.WfEvent
	err := s.conn.QueryRowsCtx(ctx, &out, "select `id`,`run_id`,`node_id`,`iteration`,`type`,`payload`,`actor`,`created_at` from `wf_event` where `run_id` = ? order by `id`", runID)
	return out, err
}

func (s *Store) dueJobs(ctx context.Context, now time.Time, limit int) ([]Job, error) {
	rows, err := s.jobs.FindDue(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, jobFromRow(r))
	}
	return out, nil
}

func (s *Store) claim(ctx context.Context, j *Job, owner string, now time.Time, lease time.Duration) (bool, error) {
	until := now.Add(lease)
	ok, err := s.jobs.Claim(ctx, uint64(j.ID), owner, now, until)
	if err != nil || !ok {
		return false, err
	}
	// 重新读取认领后的 attempt：内存里的 job 可能来自较早的轮询，attempt 已过期，
	// 而防护校验依赖每次认领得到不同的 attempt。
	row, err := s.jobs.FindOne(ctx, uint64(j.ID))
	if err != nil {
		return false, err
	}
	if row.Owner != owner {
		return false, nil
	}
	*j = jobFromRow(row)
	return true, nil
}

// ---- 已发布定义 ----

// Plans 从 wf_plan 加载已发布的定义，并在进程内缓存（发布后不可变）。
type Plans struct {
	model model.WfPlanModel
	mu    sync.RWMutex
	cache map[string]*Definition
}

// NewPlans 创建 PlanSource。
func NewPlans(conn sqlx.SqlConn) *Plans {
	return &Plans{model: model.NewWfPlanModel(conn), cache: map[string]*Definition{}}
}

// Load 实现 PlanSource。
func (p *Plans) Load(ctx context.Context, key string, version int) (*Definition, error) {
	ck := fmt.Sprintf("%s@%d", key, version)
	p.mu.RLock()
	d := p.cache[ck]
	p.mu.RUnlock()
	if d != nil {
		return d, nil
	}
	row, err := p.model.FindOneByPlanIdVersion(ctx, key, int64(version))
	if err != nil {
		return nil, fmt.Errorf("engine: load plan %s: %w", ck, err)
	}
	t, err := spec.Parse([]byte(row.Dsl))
	if err != nil {
		return nil, fmt.Errorf("engine: plan %s dsl: %w", ck, err)
	}
	var plan compiler.Plan
	if err := json.Unmarshal([]byte(row.Compiled), &plan); err != nil {
		return nil, fmt.Errorf("engine: plan %s compiled: %w", ck, err)
	}
	d = &Definition{Key: key, Version: version, Spec: t, Plan: &plan}
	p.mu.Lock()
	p.cache[ck] = d
	p.mu.Unlock()
	return d, nil
}
