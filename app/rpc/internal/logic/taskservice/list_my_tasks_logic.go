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
	return list(l.ctx, l.svcCtx, q, actor)
}

// list 查询一页任务；第一页同时返回按 (优先级, 状态) 的计数，供前端按优先级分区显示总数。
func list(ctx context.Context, svcCtx *svc.ServiceContext, q task.ListQuery, actor string) (*pb.ListMyTasksResp, error) {
	ts, next, err := svcCtx.Tasks.List(ctx, q)
	if err != nil {
		return nil, queryError(err)
	}
	b := taskview.Builder{S: svcCtx.Services}
	out := &pb.ListMyTasksResp{NextCursor: next}
	for i := range ts {
		out.Items = append(out.Items, b.Summary(ctx, &ts[i], actor))
	}
	if q.Cursor == "" {
		counts, err := svcCtx.Tasks.Count(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, c := range counts {
			out.Counts = append(out.Counts, &pb.TaskCount{
				Priority: pb.TaskPriority(task.PriorityToProto(c.Priority)),
				Status:   pb.TaskStatus(task.StatusToProto(c.Status)),
				Count:    c.N,
			})
		}
	}
	return out, nil
}
