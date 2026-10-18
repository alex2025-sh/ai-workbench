package callbackservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LarkCardCallbackLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLarkCardCallbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LarkCardCallbackLogic {
	return &LarkCardCallbackLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Lark 卡片交互：验签 -> open_id 映射 -> 按任务表鉴权 -> Signal
func (l *LarkCardCallbackLogic) LarkCardCallback(in *pb.LarkCardCallbackReq) (*pb.LarkCardCallbackResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
