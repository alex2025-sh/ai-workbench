package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SkipNodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSkipNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SkipNodeLogic {
	return &SkipNodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 人工干预：以指定端口跳过节点
func (l *SkipNodeLogic) SkipNode(in *pb.AdminNodeReq) (*pb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
