package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

// Store 是引擎的持久化层，基于 goctl 生成的模型（internal/model）。
type Store struct {
	conn  sqlx.SqlConn
	runs  model.WfRunModel
	nodes model.WfNodeRunModel
	jobs  model.WfJobModel
}

// NewStore 创建 Store。conn 需要 parseTime=true&loc=UTC。
func NewStore(conn sqlx.SqlConn) *Store {
	return &Store{
		conn:  conn,
		runs:  model.NewWfRunModel(conn),
		nodes: model.NewWfNodeRunModel(conn),
		jobs:  model.NewWfJobModel(conn),
	}
}

// tx 是一个事务内可用的模型。
type tx struct {
	ctx   context.Context
	runs  model.WfRunModel
	nodes model.WfNodeRunModel
	jobs  model.WfJobModel
	evts  model.WfEventModel
}

// transact 在一个事务内执行 fn，只有提交成功才返回 fn 的结果。
//
// 遇到死锁（1213）或锁等待超时（1205）时整体重试：每次尝试都重新加载状态、得到独立的结果，
// 回滚的那次尝试留下的结果一律丢弃。fn 内不得做外部 IO、启动 goroutine 或修改共享状态。
// 所有事务都先锁 run 行、只按主键修改 job，正常情况下不会死锁；这里是兜底，出现时记错误日志。
func transact[T any](ctx context.Context, s *Store, fn func(t *tx) (T, error)) (T, error) {
	var zero T
	for attempt := 1; ; attempt++ {
		var out T
		err := s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			conn := sqlx.NewSqlConnFromSession(session)
			var err error
			out, err = fn(&tx{
				ctx:   ctx,
				runs:  model.NewWfRunModel(conn),
				nodes: model.NewWfNodeRunModel(conn),
				jobs:  model.NewWfJobModel(conn),
				evts:  model.NewWfEventModel(conn),
			})
			return err
		})
		if err == nil {
			return out, nil
		}
		if !retryable(err) || attempt == 5 {
			return zero, err
		}
		logx.WithContext(ctx).Errorf("engine: tx retry %d: %v", attempt, err)
		select {
		case <-ctx.Done():
			return zero, err
		case <-time.After(time.Duration(10*attempt+rand.Intn(20)) * time.Millisecond):
		}
	}
}

// retryable 报告错误是否为死锁（1213）或锁等待超时（1205）。
func retryable(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1213 || me.Number == 1205
	}
	return err != nil && (strings.Contains(err.Error(), "Deadlock found") || strings.Contains(err.Error(), "Lock wait timeout"))
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

func runToRow(run Run) (*model.WfRun, error) {
	vars, err := json.Marshal(run.Vars)
	if err != nil {
		return nil, err
	}
	dry := int64(0)
	if run.DryRun {
		dry = 1
	}
	return &model.WfRun{
		Id: uint64(run.ID), PlanId: run.PlanKey, PlanVersion: int64(run.PlanVersion), BizKey: run.BizKey, Status: string(run.Status),
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
	return Job{
		ID: int64(r.Id), RunID: int64(r.RunId), NodeID: r.NodeId, Iteration: int(r.Iteration),
		Kind: JobKind(r.Kind), DueAt: r.DueAt.UTC(), Attempt: int(r.Attempt),
	}
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

func nullOr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// ---- 事务外的读取 ----

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

// ---- 已发布定义 ----

// Definition 是一个已发布的任务类型版本：完整定义、编译后的执行计划，以及运行时反复用到的预处理结果。发布后不可变。
type Definition struct {
	Key     string
	Version int
	Spec    *spec.TaskType
	Plan    *compiler.Plan

	nodes   map[string]dsl.Node
	configs map[string]any // dsl.DecodeConfig 解码后的配置，只读，多个执行共享
	modes   map[string]dsl.MergeMode
}

// NewDefinition 预处理执行计划：节点索引、解码后的配置与 merge 模式。配置无法解码时返回错误。
func NewDefinition(key string, version int, t *spec.TaskType, plan *compiler.Plan) (*Definition, error) {
	d := &Definition{Key: key, Version: version, Spec: t, Plan: plan,
		nodes: map[string]dsl.Node{}, configs: map[string]any{}, modes: mergeModes(plan)}
	for _, n := range plan.Graph.Nodes {
		cfg, err := dsl.DecodeConfig(n.Type, n.Config)
		if err != nil {
			return nil, fmt.Errorf("engine: plan %s@%d node %s: %w", key, version, n.ID, err)
		}
		d.nodes[n.ID], d.configs[n.ID] = n, cfg
	}
	return d, nil
}

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
	if d, err = NewDefinition(key, version, t, &plan); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.cache[ck] = d
	p.mu.Unlock()
	return d, nil
}
