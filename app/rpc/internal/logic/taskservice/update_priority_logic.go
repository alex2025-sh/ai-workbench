package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

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
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	p, ok := task.PriorityFromProto(int32(in.Priority))
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "bad priority %v", in.Priority)
	}
	t, err := loadTask(l.ctx, l.svcCtx, in.Id)
	if err != nil {
		return nil, err
	}
	if err := requireOwnerOrAdmin(l.ctx, l.svcCtx, t, actor); err != nil {
		return nil, err
	}
	if t, err = l.svcCtx.Tasks.UpdatePriority(l.ctx, in.Id, p, actor, in.Reason); err != nil {
		return nil, taskError(err)
	}
	return view(l.ctx, l.svcCtx, t), nil
}
