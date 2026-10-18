package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListChangeSetsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListChangeSetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListChangeSetsLogic {
	return &ListChangeSetsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 变更集列表
func (l *ListChangeSetsLogic) ListChangeSets(in *pb.ListChangeSetsReq) (*pb.ListChangeSetsResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
