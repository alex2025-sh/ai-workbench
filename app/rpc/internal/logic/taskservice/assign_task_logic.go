package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type AssignTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAssignTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssignTaskLogic {
	return &AssignTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 分配（分配模式的分配者指定执行者）
func (l *AssignTaskLogic) AssignTask(in *pb.AssignTaskReq) (*pb.TaskSummary, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	if in.AssigneeId == "" {
		return nil, rpcx.Invalid("assignee_id is required")
	}
	t, err := l.svcCtx.Tasks.Do(l.ctx, in.Id, task.Event{Kind: task.EvAssign, Actor: c.User, Assignee: in.AssigneeId})
	if err != nil {
		return nil, rpcx.Err(err)
	}
	s, err := rpcx.Summary(l.ctx, l.svcCtx, t, c)
	return s, rpcx.Err(err)
}
