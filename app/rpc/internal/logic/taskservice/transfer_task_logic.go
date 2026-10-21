package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

type TransferTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewTransferTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TransferTaskLogic {
	return &TransferTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 转交：同一节点内交给另一个人，或放回待认领（只限认领节点）。处理人（节点启用了 transfer）或 task.admin，必须写原因
func (l *TransferTaskLogic) TransferTask(in *pb.TransferTaskReq) (*pb.TaskSummary, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, rpcx.Invalid("reason is required")
	}
	t, err := l.svcCtx.Tasks.Do(l.ctx, in.Id, task.Event{Kind: task.EvTransfer, Actor: c.User, Admin: c.Admin,
		Assignee: strings.TrimSpace(in.AssigneeId), Note: reason, ExpectNode: in.ExpectNode})
	if err != nil {
		return nil, rpcx.Err(err)
	}
	s, err := rpcx.Summary(l.ctx, l.svcCtx, t, c)
	return s, rpcx.Err(err)
}
