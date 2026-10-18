package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SaveComponentDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveComponentDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveComponentDraftLogic {
	return &SaveComponentDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 保存草稿（revision 乐观锁）
func (l *SaveComponentDraftLogic) SaveComponentDraft(in *pb.SaveComponentDraftReq) (*pb.SaveComponentDraftResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
