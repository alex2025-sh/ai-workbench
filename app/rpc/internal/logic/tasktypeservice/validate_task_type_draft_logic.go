package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ValidateTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewValidateTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ValidateTaskTypeDraftLogic {
	return &ValidateTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 对草稿及其依赖闭包完整编译校验
func (l *ValidateTaskTypeDraftLogic) ValidateTaskTypeDraft(in *pb.TaskTypeKeyReq) (*pb.ValidateResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
