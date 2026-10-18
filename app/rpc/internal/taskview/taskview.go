// Package taskview 把任务中心的任务与引擎的 run 快照拼成 TaskService 的返回值：
// 列表摘要、任务视图与任务详情（渲染后的卡片、等待中的人工节点、简化进度）。
//
// 卡片与标题用发送 Lark 卡片的同一套模板解析与渲染（nodes.Template / nodes.RenderCard），
// payload 按 schema 的 x-pii 脱敏；Web 与 Lark 看到的内容一致。
package taskview

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/flow/nodes"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/schema"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

// Builder 组装 TaskService 的返回值。
type Builder struct {
	S *bootstrap.Services
}

// Summary 返回列表用的摘要。标题与副标题取任务类型默认模板的 title / summary。
func (b Builder) Summary(ctx context.Context, t *task.Task) *pb.TaskSummary {
	title, subtitle := b.titles(ctx, t)
	return &pb.TaskSummary{
		Id: t.ID, TaskTypeKey: t.TaskType, Title: title, Subtitle: subtitle,
		Status: pb.TaskStatus(task.StatusToProto(t.Status)), Priority: pb.TaskPriority(task.PriorityToProto(t.Priority)),
		AssigneeId: t.AssigneeID, SlaStatus: string(t.SLAStatus),
		SnoozeUntil: millis(t.SnoozeUntil), DueAt: millis(t.DueAt), UpdatedAt: t.UpdatedAt.UnixMilli(),
	}
}

// View 返回任务视图，payload 已脱敏。
func (b Builder) View(ctx context.Context, t *task.Task) *pb.TaskView {
	title, subtitle := b.titles(ctx, t)
	return &pb.TaskView{
		Id: t.ID, TaskTypeKey: t.TaskType, TaskTypeVersion: int32(t.TypeVersion),
		Status: pb.TaskStatus(task.StatusToProto(t.Status)), Priority: pb.TaskPriority(task.PriorityToProto(t.Priority)),
		AssigneeId: t.AssigneeID, CurrentNode: t.CurrentNode, Resolution: string(t.Resolution), SlaStatus: string(t.SLAStatus),
		SnoozeUntil: millis(t.SnoozeUntil), DueAt: millis(t.DueAt), PayloadJson: b.maskedPayload(ctx, t),
		RunId: t.RunID, UpdatedAt: t.UpdatedAt.UnixMilli(), Title: title, Subtitle: subtitle,
	}
}

// Detail 返回任务详情。actor 用于判断能否对等待中的人工节点做决定。
func (b Builder) Detail(ctx context.Context, t *task.Task, actor string) (*pb.TaskDetail, error) {
	out := &pb.TaskDetail{Task: b.View(ctx, t)}
	if t.RunID == 0 {
		def, err := b.S.Plans.Load(ctx, t.TaskType, t.TypeVersion)
		if err != nil {
			return nil, err
		}
		out.RenderedJson = render(ctx, def, def.Spec.View, taskEnv(t))
		return out, nil
	}
	snap, err := b.S.Engine.Snapshot(ctx, t.RunID)
	if err != nil {
		return nil, err
	}
	var waiting, shown *engine.NodeRun
	for i := range snap.Nodes {
		n := &snap.Nodes[i]
		node, _ := snap.Def.Node(n.NodeID)
		if node.Type == dsl.NodeHumanChoice && n.Status == engine.NodeWaiting {
			waiting = n
		}
		// 卡片取最近一个发过卡片的节点（通知或人工节点）；都没有时用任务类型默认模板。
		// 开始时间相同时取拓扑序靠后的（Nodes 已按拓扑序排列）。
		if _, ok := snap.Def.Plan.Bindings[n.NodeID]; ok && n.StartedAt != nil && n.Status != engine.NodeSkipped {
			if shown == nil || !n.StartedAt.Before(*shown.StartedAt) {
				shown = n
			}
		}
	}
	if waiting != nil {
		shown = waiting
		if out.PendingAction, err = b.pending(ctx, snap.Def, t, waiting, actor); err != nil {
			return nil, err
		}
	}
	if shown != nil {
		if out.RenderedJson = sentCard(shown.Output); out.RenderedJson == nil {
			out.RenderedJson = render(ctx, snap.Def, snap.Def.Plan.Bindings[shown.NodeID].View, snap.Env(shown.Iteration))
		}
	} else {
		out.RenderedJson = render(ctx, snap.Def, snap.Def.Spec.View, snap.Env(0))
	}
	out.Progress = progress(snap)
	return out, nil
}

func (b Builder) pending(ctx context.Context, def *engine.Definition, t *task.Task, n *engine.NodeRun, actor string) (*pb.PendingAction, error) {
	_, raw := def.Node(n.NodeID)
	cfg := raw.(*dsl.HumanChoiceConfig)
	can, err := task.CanDecide(ctx, t, actor, b.S.Groups)
	if err != nil {
		return nil, err
	}
	p := &pb.PendingAction{NodeId: n.NodeID, Iteration: int32(n.Iteration), CanSignal: can, DeadlineAt: millis(n.WaitUntil)}
	for _, o := range cfg.Options {
		opt := &pb.ActionOption{Key: o.Key, Label: o.Label, Style: o.Style, RequiredFields: o.RequiredFields}
		if o.Snooze != nil {
			opt.SnoozeMs = o.Snooze.Std().Milliseconds()
		}
		p.Options = append(p.Options, opt)
	}
	if cfg.Form != nil {
		for _, f := range cfg.Form.Fields {
			p.FormFields = append(p.FormFields, &pb.FormField{Key: f.Key, Label: f.Label, Type: f.Type, Options: f.Options})
		}
	}
	return p, nil
}

// progress 列出用户能理解的步骤：去掉触发、条件、汇合与结束这类结构性节点。
func progress(snap *engine.Snapshot) []*pb.ProgressStep {
	var out []*pb.ProgressStep
	for _, n := range snap.Nodes {
		node, _ := snap.Def.Node(n.NodeID)
		switch node.Type {
		case dsl.NodeTrigger, dsl.NodeCondition, dsl.NodeMerge, dsl.NodeEnd:
			continue
		}
		name := node.Name
		if name == "" {
			name = node.ID
		}
		status := string(n.Status)
		if n.Status == engine.NodeReady {
			status = string(engine.NodePending)
		}
		out = append(out, &pb.ProgressStep{NodeId: n.NodeID, DisplayName: name, Status: status})
	}
	return out
}

func (b Builder) titles(ctx context.Context, t *task.Task) (string, string) {
	def, err := b.S.Plans.Load(ctx, t.TaskType, t.TypeVersion)
	if err != nil {
		logx.WithContext(ctx).Errorf("taskview: load %s@%d: %v", t.TaskType, t.TypeVersion, err)
		return fmt.Sprintf("%s · %s", t.TaskType, t.SubjectID), ""
	}
	fallback := fmt.Sprintf("%s · %s", def.Spec.Name, t.SubjectID)
	tpl, err := nodes.Template(def, def.Spec.View)
	if err != nil {
		return fallback, ""
	}
	// 列表不区分查看人，也不依赖节点输出：只用 payload 与任务字段渲染。
	card, err := nodes.RenderCard(def, tpl, taskEnv(t))
	if err != nil || card.Title == "" {
		return fallback, ""
	}
	return card.Title, card.Summary
}

func (b Builder) maskedPayload(ctx context.Context, t *task.Task) []byte {
	def, err := b.S.Plans.Load(ctx, t.TaskType, t.TypeVersion)
	if err != nil {
		return nil
	}
	p, _ := expr.Decode(t.Payload)
	m, ok := p.(map[string]any)
	if !ok {
		return nil
	}
	raw, _ := json.Marshal(schema.Mask(def.Spec.Schema, m))
	return raw
}

// render 渲染卡片，按钮不放进卡片（由 PendingAction 给出）。渲染失败时返回空，详情页只显示任务字段。
func render(ctx context.Context, def *engine.Definition, b *dsl.ViewBinding, env expr.Env) []byte {
	tpl, err := nodes.Template(def, b)
	if err != nil {
		logx.WithContext(ctx).Errorf("taskview: %s@%d view: %v", def.Key, def.Version, err)
		return nil
	}
	card, err := nodes.RenderCard(def, tpl, env)
	if err != nil {
		logx.WithContext(ctx).Errorf("taskview: %s@%d render: %v", def.Key, def.Version, err)
		return nil
	}
	card.Actions, card.Callback = nil, nil
	raw, _ := json.Marshal(card)
	return raw
}

// sentCard 取节点输出中记录的、实际发出的卡片（见 docs/task-type-spec.md）；没有时返回 nil。
func sentCard(output json.RawMessage) []byte {
	var o struct {
		Card *action.Card `json:"card"`
	}
	if json.Unmarshal(output, &o) != nil || o.Card == nil {
		return nil
	}
	o.Card.Actions, o.Card.Callback = nil, nil
	raw, _ := json.Marshal(o.Card)
	return raw
}

// taskEnv 是没有 run 快照时的表达式环境：payload 与任务字段，与引擎 buildEnv 的 task 命名空间一致。
func taskEnv(t *task.Task) expr.Env {
	payload, _ := expr.Decode(t.Payload)
	return expr.Env{
		expr.RootPayload: payload,
		expr.RootTask: map[string]any{
			"id": t.ID, "type": t.TaskType, "priority": fmt.Sprintf("P%d", t.Priority),
			"assignee": t.AssigneeID, "group": t.AssigneeGroup,
		},
		expr.RootNodes: map[string]any{},
	}
}

// millis 把可空时间转为 Unix 毫秒，空为 0。
func millis(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.UnixMilli()
}
