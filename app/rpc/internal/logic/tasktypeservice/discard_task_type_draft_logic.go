package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type DiscardTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDiscardTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DiscardTaskTypeDraftLogic {
	return &DiscardTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 丢弃草稿
func (l *DiscardTaskTypeDraftLogic) DiscardTaskTypeDraft(in *pb.DiscardDraftReq) (*pb.Empty, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	if err := l.svcCtx.TaskTypes.Discard(l.ctx, in.Key, in.Revision); err != nil {
		return nil, draftError(err)
	}
	return &pb.Empty{}, nil
}
