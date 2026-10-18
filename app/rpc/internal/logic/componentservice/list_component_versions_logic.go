package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListComponentVersionsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListComponentVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListComponentVersionsLogic {
	return &ListComponentVersionsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 版本历史
func (l *ListComponentVersionsLogic) ListComponentVersions(in *pb.ComponentRefReq) (*pb.ListVersionsResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
