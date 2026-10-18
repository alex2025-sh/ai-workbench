package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdatePriorityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdatePriorityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdatePriorityLogic {
	return &UpdatePriorityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 调整优先级（原因必填，写 Task Event）
func (l *UpdatePriorityLogic) UpdatePriority(in *pb.UpdatePriorityReq) (*pb.TaskView, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
