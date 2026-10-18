package runservicelogic

import (
	"context"
	"errors"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SignalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSignalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SignalLogic {
	return &SignalLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 向等待中的人工节点发送选择：按任务表鉴权（task.CanDecide，与 Lark 卡片回调共用）后交给引擎。
func (l *SignalLogic) Signal(in *pb.SignalReq) (*pb.SignalResp, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	view, err := l.svcCtx.Engine.GetRun(l.ctx, in.RunId)
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "run %d not found", in.RunId)
	} else if err != nil {
		return nil, err
	}
	taskID := view.Run.Vars.Task.ID
	if taskID == 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "run %d is not driven by a task", in.RunId)
	}
	t, err := l.svcCtx.Tasks.Get(l.ctx, taskID)
	if err != nil {
		return nil, err
	}
	ok, err := task.CanDecide(l.ctx, t, actor, l.svcCtx.Groups)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &pb.SignalResp{Code: "forbidden", Reason: "你不是该任务的处理人"}, nil
	}
	res, err := l.svcCtx.Engine.Signal(l.ctx, engine.SignalCmd{
		RunID: in.RunId, NodeID: in.NodeId, Iteration: int(in.Iteration), Option: in.Option, Form: in.Form, Actor: actor,
	})
	if err != nil {
		return nil, err
	}
	return &pb.SignalResp{Accepted: res.Accepted, Code: string(res.Code), Reason: res.Reason}, nil
}
