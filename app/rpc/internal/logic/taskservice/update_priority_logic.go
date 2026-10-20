package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdatePriorityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdatePriorityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdatePriorityLogic {
	return &UpdatePriorityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 调整优先级（处理人或 task.admin）
func (l *UpdatePriorityLogic) UpdatePriority(in *pb.UpdatePriorityReq) (*pb.TaskSummary, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	t, err := l.svcCtx.Tasks.Do(l.ctx, in.Id, task.Event{Kind: task.EvPriority, Actor: c.User, Admin: c.Admin, Priority: task.TaskPriority(in.Priority)})
	if err != nil {
		return nil, rpcx.Err(err)
	}
	s, err := rpcx.Summary(l.ctx, l.svcCtx, t, c)
	return s, rpcx.Err(err)
}
