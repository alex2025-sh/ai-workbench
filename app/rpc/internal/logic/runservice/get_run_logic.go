package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetRunLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRunLogic {
	return &GetRunLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 运行状态，供运行态画布着色
func (l *GetRunLogic) GetRun(in *pb.RunIdReq) (*pb.RunView, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
