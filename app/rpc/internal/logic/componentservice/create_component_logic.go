package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateComponentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateComponentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateComponentLogic {
	return &CreateComponentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 新建共享组件并创建草稿
func (l *CreateComponentLogic) CreateComponent(in *pb.CreateComponentReq) (*pb.CreateComponentResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
