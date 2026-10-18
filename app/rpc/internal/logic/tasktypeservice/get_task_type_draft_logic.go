package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
