package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
