package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetRunEventsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetRunEventsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRunEventsLogic {
	return &GetRunEventsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 运行事件时间线
func (l *GetRunEventsLogic) GetRunEvents(in *pb.GetRunEventsReq) (*pb.GetRunEventsResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
