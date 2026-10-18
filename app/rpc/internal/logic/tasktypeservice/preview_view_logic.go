package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PreviewViewLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPreviewViewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreviewViewLogic {
	return &PreviewViewLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 用样例数据预览卡片模板
func (l *PreviewViewLogic) PreviewView(in *pb.PreviewViewReq) (*pb.PreviewViewResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
