package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateTaskTypeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateTaskTypeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTaskTypeLogic {
	return &CreateTaskTypeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 新建任务类型并创建草稿
func (l *CreateTaskTypeLogic) CreateTaskType(in *pb.CreateTaskTypeReq) (*pb.CreateTaskTypeResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
