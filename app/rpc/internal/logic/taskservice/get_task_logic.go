package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskLogic {
	return &GetTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 任务详情：卡片、业务字段、节点链、历史，以及调用人可做的操作
func (l *GetTaskLogic) GetTask(in *pb.TaskIdReq) (*pb.TaskDetail, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	t, err := l.svcCtx.Tasks.Get(l.ctx, in.Id)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	d, err := rpcx.Detail(l.ctx, l.svcCtx, t, c)
	return d, rpcx.Err(err)
}
