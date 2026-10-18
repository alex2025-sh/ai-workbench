package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetComponentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetComponentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetComponentLogic {
	return &GetComponentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取已发布版本
func (l *GetComponentLogic) GetComponent(in *pb.GetComponentReq) (*pb.GetComponentResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
