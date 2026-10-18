package taskservicelogic

import (
	"context"
	"strings"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReassignLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReassignLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReassignLogic {
	return &ReassignLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 转派（只改变处理人，状态不变）
func (l *ReassignLogic) Reassign(in *pb.ReassignReq) (*pb.TaskView, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.AssigneeId) == "" {
		return nil, status.Error(codes.InvalidArgument, "assignee_id is required")
	}
	t, err := loadTask(l.ctx, l.svcCtx, in.Id)
	if err != nil {
		return nil, err
	}
	if err := requireOwnerOrAdmin(l.ctx, l.svcCtx, t, actor); err != nil {
		return nil, err
	}
	if t, err = l.svcCtx.Tasks.Reassign(l.ctx, in.Id, strings.TrimSpace(in.AssigneeId), actor, in.Reason); err != nil {
		return nil, taskError(err)
	}
	return view(l.ctx, l.svcCtx, t), nil
}
