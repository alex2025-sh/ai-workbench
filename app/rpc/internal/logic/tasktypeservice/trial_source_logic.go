package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type TrialSourceLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewTrialSourceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TrialSourceLogic {
	return &TrialSourceLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 试跑来源：cron 立即扫描一次，Kafka / API 用样例消息
func (l *TrialSourceLogic) TrialSource(in *pb.TrialSourceReq) (*pb.TrialSourceResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
