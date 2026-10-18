package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type StartProcessingLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewStartProcessingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartProcessingLogic {
	return &StartProcessingLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 开始处理（pending -> processing，不影响流程）
func (l *StartProcessingLogic) StartProcessing(in *pb.TaskIdReq) (*pb.TaskView, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	t, err := l.svcCtx.Tasks.StartProcessing(l.ctx, in.Id, actor)
	if err != nil {
		return nil, taskError(err)
	}
	return view(l.ctx, l.svcCtx, t), nil
}
