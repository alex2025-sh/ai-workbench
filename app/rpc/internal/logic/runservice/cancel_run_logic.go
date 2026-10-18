package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CancelRunLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCancelRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelRunLogic {
	return &CancelRunLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 取消运行
func (l *CancelRunLogic) CancelRun(in *pb.CancelRunReq) (*pb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
