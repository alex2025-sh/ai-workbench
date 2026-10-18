package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type StartProcessingLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewStartProcessingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartProcessingLogic {
	return &StartProcessingLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 开始处理（pending -> processing，不影响流程）
func (l *StartProcessingLogic) StartProcessing(in *pb.TaskIdReq) (*pb.TaskView, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
