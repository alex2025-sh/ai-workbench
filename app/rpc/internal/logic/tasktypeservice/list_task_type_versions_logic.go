package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListTaskTypeVersionsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListTaskTypeVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTaskTypeVersionsLogic {
	return &ListTaskTypeVersionsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 版本历史
func (l *ListTaskTypeVersionsLogic) ListTaskTypeVersions(in *pb.TaskTypeKeyReq) (*pb.ListVersionsResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
