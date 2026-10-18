package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetComponentDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetComponentDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetComponentDraftLogic {
	return &GetComponentDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取草稿；没有时基于最新发布版本创建
func (l *GetComponentDraftLogic) GetComponentDraft(in *pb.ComponentRefReq) (*pb.GetComponentDraftResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
