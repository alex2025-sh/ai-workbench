package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ClaimTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewClaimTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClaimTaskLogic {
	return &ClaimTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 认领组任务（unassigned -> pending，CAS）
func (l *ClaimTaskLogic) ClaimTask(in *pb.TaskIdReq) (*pb.TaskView, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	t, err := loadTask(l.ctx, l.svcCtx, in.Id)
	if err != nil {
		return nil, err
	}
	if t.AssigneeGroup != "" {
		ok, err := l.svcCtx.Groups.IsMember(l.ctx, t.AssigneeGroup, actor)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, status.Errorf(codes.PermissionDenied, "not a member of group %s", t.AssigneeGroup)
		}
	}
	if t, err = l.svcCtx.Tasks.Claim(l.ctx, in.Id, actor); err != nil {
		return nil, taskError(err)
	}
	return view(l.ctx, l.svcCtx, t), nil
}
