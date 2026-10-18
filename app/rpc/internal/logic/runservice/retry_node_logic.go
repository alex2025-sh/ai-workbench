package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type RetryNodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRetryNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RetryNodeLogic {
	return &RetryNodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 人工干预：重试节点
func (l *RetryNodeLogic) RetryNode(in *pb.AdminNodeReq) (*pb.Empty, error) {
	actor, err := adminActor(l.ctx, l.svcCtx, in.Reason)
	if err != nil {
		return nil, err
	}
	if err := l.svcCtx.Engine.RetryNode(l.ctx, in.RunId, in.NodeId, actor, in.Reason); err != nil {
		return nil, interveneError(err, in.RunId)
	}
	return &pb.Empty{}, nil
}
