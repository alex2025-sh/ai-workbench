package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListMyTasksLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMyTasksLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMyTasksLogic {
	return &ListMyTasksLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 我的任务，按优先级、截止时间排序；返回轻量摘要，不含 payload
func (l *ListMyTasksLogic) ListMyTasks(in *pb.ListMyTasksReq) (*pb.ListMyTasksResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
