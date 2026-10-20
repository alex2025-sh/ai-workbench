package tasktypeservicelogic

import (
	"context"

	"errors"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
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

// 发布草稿为新版本（需要 task_type.publish 权限；有问题时不发布）
func (l *PublishTaskTypeDraftLogic) PublishTaskTypeDraft(in *pb.TaskTypeKeyReq) (*pb.PublishResp, error) {
	user, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := identity.RequirePermission(l.ctx, l.svcCtx.Authz, user, identity.PermPublish); err != nil {
		return nil, err
	}
	v, issues, err := l.svcCtx.Registry.Publish(l.ctx, in.Key, user, time.Now())
	if errors.Is(err, registry.ErrInvalidDraft) {
		return &pb.PublishResp{Issues: rpcx.Issues(issues)}, nil
	}
	if err != nil {
		return nil, rpcx.Err(err)
	}
	return &pb.PublishResp{Version: &pb.VersionInfo{Version: int32(v.Version), ContentHash: v.ContentHash, PublishedBy: v.PublishedBy,
		PublishedAt: v.PublishedAt.UnixMilli()}}, nil
}
