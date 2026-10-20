// Package rpcx 是各 logic 共用的转换：领域错误 → gRPC 状态码、任务 → TaskSummary / TaskDetail。
package rpcx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/source"
	"github.com/alex2025-sh/ai-workbench/internal/task"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
	"github.com/alex2025-sh/ai-workbench/internal/team"
)

// Err 把领域错误映射为 gRPC 状态码；已经是 status 的错误原样返回。
func Err(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	code := codes.Internal
	switch {
	case errors.Is(err, task.ErrNotFound), errors.Is(err, registry.ErrNotFound), errors.Is(err, team.ErrNotFound), errors.Is(err, registry.ErrUnpublished):
		code = codes.NotFound
	case errors.Is(err, task.ErrForbidden):
		code = codes.PermissionDenied
	case errors.Is(err, task.ErrState), errors.Is(err, registry.ErrInvalidDraft):
		code = codes.FailedPrecondition
	case errors.Is(err, task.ErrStale), errors.Is(err, registry.ErrConflict):
		code = codes.Aborted
	case errors.Is(err, registry.ErrExists):
		code = codes.AlreadyExists
	case errors.Is(err, source.ErrBadEnvelope):
		code = codes.InvalidArgument
	}
	return status.Error(code, err.Error())
}

// Invalid 返回 InvalidArgument。
func Invalid(format string, args ...any) error {
	return status.Errorf(codes.InvalidArgument, format, args...)
}

// Caller 是调用人：身份与是否有 task.admin。
type Caller struct {
	User  string
	Admin bool
}

// RequireCaller 读取调用人。
func RequireCaller(ctx context.Context, s *svc.ServiceContext) (Caller, error) {
	u, err := identity.Require(ctx)
	if err != nil {
		return Caller{}, err
	}
	admin, err := identity.Allowed(ctx, s.Authz, u, identity.PermTaskAdmin)
	return Caller{User: u, Admin: admin}, err
}

// Millis 把可空时间转为 Unix 毫秒，空为 0。
func Millis(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.UnixMilli()
}

// Issues 转换校验问题。
func Issues(in []tasktype.Issue) []*pb.Issue {
	out := make([]*pb.Issue, 0, len(in))
	for _, i := range in {
		out = append(out, &pb.Issue{Path: i.Path, Message: i.Message})
	}
	return out
}

// CardPB 转换卡片。
func CardPB(c tasktype.RenderedCard) *pb.Card {
	out := &pb.Card{Title: c.Title, NodeName: c.NodeName}
	for _, f := range c.Fields {
		out.Fields = append(out.Fields, &pb.KV{Label: f.Label, Value: f.Value})
	}
	if c.Link != nil {
		out.Link = &pb.KV{Label: c.Link.Label, Value: c.Link.Value}
	}
	return out
}

// ActionLabels 是动作的按钮文字（link 用节点配置的文字）。
var ActionLabels = map[string]string{
	tasktype.ActionClose:      "已处理",
	tasktype.ActionNoResponse: "无需处理",
	tasktype.ActionSnooze:     "推迟",
	tasktype.ActionEscalate:   "升级",
}

// Perms 是调用人对任务的操作权限。
type Perms struct {
	View, Act, Claim, Assign, Start bool
	Role                            string
}

// PermsOf 求调用人对任务的操作权限，规则与 task.Apply 一致（Apply 仍会再校验）。
func PermsOf(ctx context.Context, s *svc.ServiceContext, t *task.Task, c Caller) (Perms, error) {
	acc, err := s.Tasks.AccessOf(ctx, t, c.User, c.Admin)
	if err != nil {
		return Perms{}, err
	}
	p := Perms{View: acc.View, Role: acc.Role}
	if t.Status.IsTerminal() {
		return p, nil
	}
	claimable := t.InQueue() && ((t.TeamMode == tasktype.ModeAssign && acc.Role == team.RoleAssigner) || (t.TeamMode != tasktype.ModeAssign && acc.Role != ""))
	p.Claim = claimable
	p.Assign = t.InQueue() && t.TeamMode == tasktype.ModeAssign && acc.Role == team.RoleAssigner
	p.Act = t.AssigneeID == c.User || claimable
	p.Start = t.Status == task.TaskStatusPending && t.AssigneeID == c.User
	return p, nil
}

// Summary 组装 TaskSummary。
func Summary(ctx context.Context, s *svc.ServiceContext, t *task.Task, c Caller) (*pb.TaskSummary, error) {
	def, err := s.Registry.Load(ctx, t.TaskType, t.TypeVersion)
	if err != nil {
		return nil, err
	}
	p, err := PermsOf(ctx, s, t, c)
	if err != nil {
		return nil, err
	}
	return summary(def, t, p), nil
}

func summary(def *tasktype.TaskType, t *task.Task, p Perms) *pb.TaskSummary {
	vals := tasktype.DecodeValues(t.Fields)
	out := &pb.TaskSummary{
		Id: t.ID, TaskTypeKey: t.TaskType, TaskTypeName: def.Name, TaskTypeVersion: int32(t.TypeVersion), BizKey: t.BizKey,
		Status: pb.TaskStatus(task.StatusToProto(t.Status)), Priority: pb.TaskPriority(task.PriorityToProto(t.Priority)),
		AssigneeId: t.AssigneeID, TeamKey: t.TeamKey, TeamMode: t.TeamMode, NodeId: t.NodeID, Resolution: string(t.Resolution),
		SnoozeUntil: Millis(t.SnoozeUntil), CreatedAt: t.CreatedAt.UnixMilli(), UpdatedAt: t.UpdatedAt.UnixMilli(),
		Card:     CardPB(def.RenderCard(t.NodeID, t.BizKey, vals, "")),
		CanClaim: p.Claim, CanAssign: p.Assign, CanStart: p.Start,
	}
	node, _, ok := def.Node(t.NodeID)
	if !ok {
		return out
	}
	for _, a := range tasktype.Actions {
		if !node.Has(a) {
			continue
		}
		switch a {
		case tasktype.ActionLink:
			if node.Link != nil && p.View {
				out.Actions = append(out.Actions, &pb.ActionOption{Key: a, Label: node.Link.Label, Url: out.Card.GetLink().GetValue()})
			}
			continue
		}
		if !p.Act {
			continue
		}
		o := &pb.ActionOption{Key: a, Label: ActionLabels[a]}
		switch a {
		case tasktype.ActionSnooze:
			for _, d := range node.SnoozeOptions() {
				if v, err := tasktype.ParseDuration(d); err == nil {
					o.SnoozeMs = append(o.SnoozeMs, v.Milliseconds())
				}
			}
		case tasktype.ActionEscalate:
			if next, ok := def.Next(node.ID); ok {
				o.NextNodeName = next.Name
			}
		}
		out.Actions = append(out.Actions, o)
	}
	return out
}

// Detail 组装 TaskDetail。
func Detail(ctx context.Context, s *svc.ServiceContext, t *task.Task, c Caller) (*pb.TaskDetail, error) {
	def, err := s.Registry.Load(ctx, t.TaskType, t.TypeVersion)
	if err != nil {
		return nil, err
	}
	p, err := PermsOf(ctx, s, t, c)
	if err != nil {
		return nil, err
	}
	if !p.View {
		return nil, status.Error(codes.PermissionDenied, "you are not involved in this task")
	}
	hist, err := s.Tasks.History(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	out := &pb.TaskDetail{
		Task:              summary(def, t, p),
		CanReassign:       !t.Status.IsTerminal() && (t.AssigneeID == c.User || c.Admin),
		CanUpdatePriority: !t.Status.IsTerminal() && (t.AssigneeID == c.User || c.Admin),
	}
	out.ReadOnly = !p.Act && !p.Claim && !out.CanReassign
	for _, kv := range def.Masked(t.BizKey, tasktype.DecodeValues(t.Fields)) {
		out.Fields = append(out.Fields, &pb.KV{Label: kv.Label, Value: kv.Value})
	}
	visited := map[string]bool{t.NodeID: true}
	for _, h := range hist {
		visited[h.NodeID] = true
		name := h.NodeID
		if n, _, ok := def.Node(h.NodeID); ok {
			name = n.Name
		}
		out.History = append(out.History, &pb.HistoryItem{Kind: h.Kind, Actor: h.Actor, NodeId: h.NodeID, NodeName: name,
			FromStatus: pb.TaskStatus(task.StatusToProto(h.FromStatus)), ToStatus: pb.TaskStatus(task.StatusToProto(h.ToStatus)),
			DetailJson: h.Detail, At: h.At.UnixMilli()})
	}
	for _, n := range def.Nodes {
		out.Nodes = append(out.Nodes, &pb.NodeView{Id: n.ID, Name: n.Name, Assignee: AssigneeText(def, n.Assignee),
			Current: n.ID == t.NodeID && !t.Status.IsTerminal(), Visited: visited[n.ID], Actions: n.Actions})
	}
	if p.Assign {
		if tm, err := s.Teams.Get(ctx, t.TeamKey); err == nil {
			out.TeamMembers = tm.Executors()
		}
	}
	return out, nil
}

// AssigneeText 是分配器的文字说明。
func AssigneeText(def *tasktype.TaskType, a tasktype.Assignee) string {
	switch a.Kind {
	case tasktype.AssigneeRelation:
		field := a.Field
		if f, ok := def.Field(a.Field); ok && f.Label != "" {
			field = f.Label
		}
		return fmt.Sprintf("业务关系 %s（%s），查不到时进入团队 %s", a.Relation, field, a.FallbackTeam)
	case tasktype.AssigneeTeam:
		if a.Mode == tasktype.ModeAssign {
			return fmt.Sprintf("团队 %s · 分配者指定", a.Team)
		}
		return fmt.Sprintf("团队 %s · 认领", a.Team)
	}
	return ""
}
