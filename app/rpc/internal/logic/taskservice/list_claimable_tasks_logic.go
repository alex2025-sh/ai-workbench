package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListClaimableTasksLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListClaimableTasksLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListClaimableTasksLogic {
	return &ListClaimableTasksLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 可认领的任务池（unassigned，调用人在候选人或候选组内），排序同上
func (l *ListClaimableTasksLogic) ListClaimableTasks(in *pb.ListClaimableTasksReq) (*pb.ListMyTasksResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
