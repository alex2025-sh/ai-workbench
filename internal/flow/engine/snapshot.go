package engine

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

// Snapshot 是 run 的只读快照：已发布的定义、节点状态与表达式环境，供查询接口（任务详情、运行监控）使用。
// 读的是已提交的状态，不加锁，不改变任何东西。
type Snapshot struct {
	Run   Run
	Def   *Definition
	Nodes []NodeRun // 按 Def.Plan.Order 排列
	// Errors 是每个节点最近一次执行失败的错误（之后重试成功的也保留），取自事件日志。
	Errors map[string]string
	// Blocked 是 run 需要人工介入时卡住的节点与原因；其余状态为 nil。
	Blocked *Blocked

	rows map[string]*model.WfNodeRun
}

// Snapshot 读取 run 的只读快照。
func (e *Engine) Snapshot(ctx context.Context, runID int64) (*Snapshot, error) {
	r, err := e.store.runs.FindOne(ctx, uint64(runID))
	if err != nil {
		return nil, err
	}
	run, err := runFromRow(r)
	if err != nil {
		return nil, err
	}
	def, err := e.plans.Load(ctx, run.PlanKey, run.PlanVersion)
	if err != nil {
		return nil, err
	}
	rows, err := e.store.nodes.FindByRun(ctx, uint64(runID))
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Run: run, Def: def, rows: make(map[string]*model.WfNodeRun, len(rows))}
	for _, row := range rows {
		s.rows[row.NodeId] = row
	}
	for _, id := range def.Plan.Order {
		if row := s.rows[id]; row != nil {
			s.Nodes = append(s.Nodes, nodeFromRow(row))
		}
	}
	evts, err := model.NewWfEventModel(e.store.conn).ListByTypes(ctx, []uint64{uint64(runID)}, diagnosticEvents)
	if err != nil {
		return nil, err
	}
	s.Errors = make(map[string]string)
	for _, ev := range evts {
		if msg := eventError(ev); msg != "" && ev.NodeId != "" && ev.Type != eventNeedsIntervention {
			s.Errors[ev.NodeId] = msg
		}
	}
	if run.Status == RunNeedsIntervention {
		s.Blocked = blockedBy(evts)[runID]
	}
	return s, nil
}

// Env 返回节点在第 iteration 轮可见的表达式环境，与执行时构造的一致。
func (s *Snapshot) Env(iteration int) expr.Env { return buildEnv(s.Run, s.rows, iteration) }

// Node 返回节点定义与解码后的配置（只读）。
func (d *Definition) Node(id string) (dsl.Node, any) { return d.node(id) }
