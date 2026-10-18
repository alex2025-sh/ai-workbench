package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type StartRunLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewStartRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartRunLogic {
	return &StartRunLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 启动运行（按 biz_key 幂等）
func (l *StartRunLogic) StartRun(in *pb.StartRunReq) (*pb.StartRunResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
