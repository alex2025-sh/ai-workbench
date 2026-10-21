package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/cursor"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListTasksLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListTasksLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTasksLogic {
	return &ListTasksLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 任务列表：我的任务、待认领（所有人可见）、我经手的
func (l *ListTasksLogic) ListTasks(in *pb.ListTasksReq) (*pb.ListTasksResp, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	q := task.ListQuery{Scope: in.Scope, User: c.User, TaskType: in.TaskType, Limit: cursor.Size(int(in.GetPage().GetPageSize()))}
	switch q.Scope {
	case "", task.ScopeMine:
		q.Scope = task.ScopeMine
	case task.ScopeInvolved, task.ScopeClaimable:
	default:
		return nil, rpcx.Invalid("scope must be mine, claimable or involved")
	}
	for _, s := range in.Statuses {
		v, ok := task.StatusFromProto(int32(s))
		if !ok {
			return nil, rpcx.Invalid("invalid status %v", s)
		}
		q.Statuses = append(q.Statuses, v)
	}
	for _, p := range in.Priorities {
		v, ok := task.PriorityFromProto(int32(p))
		if !ok {
			return nil, rpcx.Invalid("invalid priority %v", p)
		}
		q.Priorities = append(q.Priorities, v)
	}
	first := in.GetPage().GetCursor() == ""
	if !first {
		keys, err := cursor.Decode(in.Page.Cursor, 2)
		if err != nil {
			return nil, rpcx.Invalid("bad cursor")
		}
		q.After = &model.TaskCursor{Priority: keys[0], ID: uint64(keys[1])}
	}
	limit := q.Limit
	q.Limit = limit + 1
	tasks, err := l.svcCtx.Tasks.List(l.ctx, q)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	out := &pb.ListTasksResp{}
	if len(tasks) > limit {
		tasks = tasks[:limit]
		last := tasks[limit-1]
		out.NextCursor = cursor.Encode(int64(last.Priority), last.ID)
	}
	for i := range tasks {
		s, err := rpcx.Summary(l.ctx, l.svcCtx, &tasks[i], c)
		if err != nil {
			return nil, rpcx.Err(err)
		}
		out.Items = append(out.Items, s)
	}
	if first {
		counts, err := l.svcCtx.Tasks.Count(l.ctx, q)
		if err != nil {
			return nil, rpcx.Err(err)
		}
		for _, n := range counts {
			out.Counts = append(out.Counts, &pb.TaskCount{Priority: pb.TaskPriority(task.PriorityToProto(n.Priority)),
				Status: pb.TaskStatus(task.StatusToProto(n.Status)), Count: n.N})
		}
	}
	return out, nil
}
