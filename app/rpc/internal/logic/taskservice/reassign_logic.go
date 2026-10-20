package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReassignLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReassignLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReassignLogic {
	return &ReassignLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 转派（处理人或 task.admin）
func (l *ReassignLogic) Reassign(in *pb.ReassignReq) (*pb.TaskSummary, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	if in.AssigneeId == "" || in.Reason == "" {
		return nil, rpcx.Invalid("assignee_id and reason are required")
	}
	t, err := l.svcCtx.Tasks.Do(l.ctx, in.Id, task.Event{Kind: task.EvReassign, Actor: c.User, Admin: c.Admin, Assignee: in.AssigneeId, Note: in.Reason})
	if err != nil {
		return nil, rpcx.Err(err)
	}
	s, err := rpcx.Summary(l.ctx, l.svcCtx, t, c)
	return s, rpcx.Err(err)
}
