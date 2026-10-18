package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
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
func (l *GetTaskTypeDraftLogic) GetTaskTypeDraft(in *pb.TaskTypeKeyReq) (*pb.GetTaskTypeDraftResp, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	d, err := l.svcCtx.TaskTypes.Get(l.ctx, in.Key, actor)
	if err != nil {
		return nil, draftError(err)
	}
	return &pb.GetTaskTypeDraftResp{Draft: toDraft(d)}, nil
}
