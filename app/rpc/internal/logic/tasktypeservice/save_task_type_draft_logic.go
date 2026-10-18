package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SaveTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveTaskTypeDraftLogic {
	return &SaveTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 保存草稿（revision 乐观锁）
func (l *SaveTaskTypeDraftLogic) SaveTaskTypeDraft(in *pb.SaveTaskTypeDraftReq) (*pb.SaveTaskTypeDraftResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
