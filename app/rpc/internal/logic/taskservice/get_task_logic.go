package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/taskview"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskLogic {
	return &GetTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 任务详情：任务数据、按调用人受众渲染的 web 卡片、当前可执行的流程操作与简化进度（均已按调用人脱敏）
func (l *GetTaskLogic) GetTask(in *pb.TaskIdReq) (*pb.TaskDetail, error) {
	// 查看权限暂按接口级权限（BSP）控制：已认证的调用人都能查看；能否做决定见 PendingAction.can_signal。
	actor, err := caller(l.ctx)
	if err != nil {
		return nil, err
	}
	t, err := loadTask(l.ctx, l.svcCtx, in.Id)
	if err != nil {
		return nil, err
	}
	return taskview.Builder{S: l.svcCtx.Services}.Detail(l.ctx, t, actor)
}
