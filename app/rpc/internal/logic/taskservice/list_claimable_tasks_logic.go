package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListClaimableTasksLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListClaimableTasksLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListClaimableTasksLogic {
	return &ListClaimableTasksLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 可认领的任务池（unassigned，调用人在候选人或候选组内），排序同上
func (l *ListClaimableTasksLogic) ListClaimableTasks(in *pb.ListClaimableTasksReq) (*pb.ListMyTasksResp, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	q, err := listQuery(nil, in.Priorities, in.TaskTypeKey, in.Page)
	if err != nil {
		return nil, err
	}
	if q.Groups, err = l.svcCtx.Groups.GroupsOf(l.ctx, actor); err != nil {
		return nil, err
	}
	if len(q.Groups) == 0 {
		return &pb.ListMyTasksResp{}, nil
	}
	q.Statuses = []task.TaskStatus{task.TaskStatusUnassigned}
	return list(l.ctx, l.svcCtx, q, actor)
}
