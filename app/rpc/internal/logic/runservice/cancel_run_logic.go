package runservicelogic

import (
	"context"
	"fmt"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CancelRunLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCancelRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelRunLogic {
	return &CancelRunLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 取消运行
func (l *CancelRunLogic) CancelRun(in *pb.CancelRunReq) (*pb.Empty, error) {
	actor, err := adminActor(l.ctx, l.svcCtx, in.Reason)
	if err != nil {
		return nil, err
	}
	// 已结束的 run 不受影响；任务经同步置为 solved（auto_closed）。
	if err := l.svcCtx.Engine.Cancel(l.ctx, in.RunId, actor, in.Reason); err != nil {
		return nil, queryError(err, fmt.Sprintf("run %d", in.RunId))
	}
	return &pb.Empty{}, nil
}
