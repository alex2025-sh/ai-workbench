package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ValidateComponentDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewValidateComponentDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ValidateComponentDraftLogic {
	return &ValidateComponentDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 校验草稿，并检查对引用方的影响
func (l *ValidateComponentDraftLogic) ValidateComponentDraft(in *pb.ComponentRefReq) (*pb.ValidateResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
