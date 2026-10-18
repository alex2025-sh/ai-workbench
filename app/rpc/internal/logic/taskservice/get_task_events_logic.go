package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetTaskEventsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskEventsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskEventsLogic {
	return &GetTaskEventsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 任务事件时间线
func (l *GetTaskEventsLogic) GetTaskEvents(in *pb.GetTaskEventsReq) (*pb.GetTaskEventsResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
