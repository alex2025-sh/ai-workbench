package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskTypeDraftLogic {
	return &GetTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取草稿；没有时基于最新发布版本创建
func (l *GetTaskTypeDraftLogic) GetTaskTypeDraft(in *pb.TaskTypeKeyReq) (*pb.DraftResp, error) {
	user, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	d, err := l.svcCtx.Registry.GetDraft(l.ctx, in.Key, user)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	_, issues := l.svcCtx.Registry.Validate(l.ctx, d.Body)
	return &pb.DraftResp{Key: d.Key, BaseVersion: int32(d.BaseVersion), Revision: d.Revision, BodyJson: d.Body, UpdatedBy: d.UpdatedBy,
		Issues: rpcx.Issues(issues)}, nil
}
