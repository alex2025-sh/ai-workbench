package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GenerateSamplePayloadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGenerateSamplePayloadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GenerateSamplePayloadLogic {
	return &GenerateSamplePayloadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 按 schema 生成或从历史任务取样例 payload
func (l *GenerateSamplePayloadLogic) GenerateSamplePayload(in *pb.GenerateSamplePayloadReq) (*pb.GenerateSamplePayloadResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
