package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReassignLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReassignLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReassignLogic {
	return &ReassignLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 转派（只改变处理人，状态不变）
func (l *ReassignLogic) Reassign(in *pb.ReassignReq) (*pb.TaskView, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
