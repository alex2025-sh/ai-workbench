package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListTaskTypesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListTaskTypesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTaskTypesLogic {
	return &ListTaskTypesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 任务类型列表，按 namespace 筛选
func (l *ListTaskTypesLogic) ListTaskTypes(in *pb.ListTaskTypesReq) (*pb.ListTaskTypesResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
