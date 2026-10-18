package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/taskview"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListMyTasksLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMyTasksLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMyTasksLogic {
	return &ListMyTasksLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 我的任务，按优先级、截止时间排序；返回轻量摘要，不含 payload
func (l *ListMyTasksLogic) ListMyTasks(in *pb.ListMyTasksReq) (*pb.ListMyTasksResp, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	q, err := listQuery(in.Statuses, in.Priorities, in.TaskTypeKey, in.Page)
	if err != nil {
		return nil, err
	}
	q.AssigneeID = actor
	return list(l.ctx, l.svcCtx, q)
}

func list(ctx context.Context, svcCtx *svc.ServiceContext, q task.ListQuery) (*pb.ListMyTasksResp, error) {
	ts, next, err := svcCtx.Tasks.List(ctx, q)
	if err != nil {
		return nil, queryError(err)
	}
	b := taskview.Builder{S: svcCtx.Services}
	out := &pb.ListMyTasksResp{NextCursor: next}
	for i := range ts {
		out.Items = append(out.Items, b.Summary(ctx, &ts[i]))
	}
	return out, nil
}
