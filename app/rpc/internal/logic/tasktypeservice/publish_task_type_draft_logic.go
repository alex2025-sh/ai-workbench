package tasktypeservicelogic

import (
	"context"
	"errors"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PublishTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPublishTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PublishTaskTypeDraftLogic {
	return &PublishTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 仅开发环境：把草稿直接发布为新版本，跳过变更集与审批（正式发布走 ChangeSetService）。
func (l *PublishTaskTypeDraftLogic) PublishTaskTypeDraft(in *pb.PublishTaskTypeDraftReq) (*pb.PublishTaskTypeDraftResp, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	if !l.svcCtx.Config.AllowDirectPublish {
		return nil, status.Error(codes.FailedPrecondition, "direct publish is disabled; publish through a change set")
	}
	if err := identity.RequirePermission(l.ctx, l.svcCtx.Authz, actor, identity.PermPublish); err != nil {
		return nil, err
	}
	v, issues, err := l.svcCtx.TaskTypes.PublishDraft(l.ctx, in.Key, in.Revision, actor)
	if errors.Is(err, registry.ErrHasErrors) {
		return &pb.PublishTaskTypeDraftResp{Issues: toIssues(issues)}, nil
	}
	if err != nil {
		return nil, draftError(err)
	}
	return &pb.PublishTaskTypeDraftResp{Version: int32(v), Issues: toIssues(issues)}, nil
}
