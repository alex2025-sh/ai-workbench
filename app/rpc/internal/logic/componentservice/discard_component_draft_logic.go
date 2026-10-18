package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DiscardComponentDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDiscardComponentDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DiscardComponentDraftLogic {
	return &DiscardComponentDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 丢弃草稿
func (l *DiscardComponentDraftLogic) DiscardComponentDraft(in *pb.DiscardComponentDraftReq) (*pb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
