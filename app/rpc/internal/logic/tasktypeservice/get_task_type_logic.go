package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetTaskTypeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskTypeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskTypeLogic {
	return &GetTaskTypeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取已发布版本及其发布清单
func (l *GetTaskTypeLogic) GetTaskType(in *pb.GetTaskTypeReq) (*pb.GetTaskTypeResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
