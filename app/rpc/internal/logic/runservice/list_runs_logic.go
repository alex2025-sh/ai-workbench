package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListRunsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListRunsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRunsLogic {
	return &ListRunsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 按任务、biz_key、状态查运行
func (l *ListRunsLogic) ListRuns(in *pb.ListRunsReq) (*pb.ListRunsResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
