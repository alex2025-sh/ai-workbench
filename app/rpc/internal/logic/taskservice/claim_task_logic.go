package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type ClaimTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewClaimTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClaimTaskLogic {
	return &ClaimTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 认领（认领模式的执行者，或分配模式的分配者领给自己）
func (l *ClaimTaskLogic) ClaimTask(in *pb.TaskIdReq) (*pb.TaskSummary, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	t, err := l.svcCtx.Tasks.Do(l.ctx, in.Id, task.Event{Kind: task.EvClaim, Actor: c.User})
	if err != nil {
		return nil, rpcx.Err(err)
	}
	s, err := rpcx.Summary(l.ctx, l.svcCtx, t, c)
	return s, rpcx.Err(err)
}
