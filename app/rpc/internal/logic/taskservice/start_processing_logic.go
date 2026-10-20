package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
)

type StartProcessingLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewStartProcessingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartProcessingLogic {
	return &StartProcessingLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 开始处理
func (l *StartProcessingLogic) StartProcessing(in *pb.TaskIdReq) (*pb.TaskSummary, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	t, err := l.svcCtx.Tasks.Do(l.ctx, in.Id, task.Event{Kind: task.EvStart, Actor: c.User})
	if err != nil {
		return nil, rpcx.Err(err)
	}
	s, err := rpcx.Summary(l.ctx, l.svcCtx, t, c)
	return s, rpcx.Err(err)
}
