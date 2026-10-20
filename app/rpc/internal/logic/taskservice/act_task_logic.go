package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/task"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type ActTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewActTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActTaskLogic {
	return &ActTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 节点动作：close、no_response、snooze、escalate（link 在前端打开，不经过后端）
func (l *ActTaskLogic) ActTask(in *pb.ActTaskReq) (*pb.TaskSummary, error) {
	c, err := rpcx.RequireCaller(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	ev := task.Event{Kind: task.EvAct, Actor: c.User, Action: in.Action, Note: in.Note, ExpectNode: in.ExpectNode,
		Snooze: time.Duration(in.SnoozeMs) * time.Millisecond}
	switch in.Action {
	case tasktype.ActionClose, tasktype.ActionNoResponse, tasktype.ActionSnooze:
	case tasktype.ActionEscalate:
		// 下一个节点的处理人在事务外求好；任务已离开调用人看到的节点时由 Apply 拒绝（ErrStale）。
		t, err := l.svcCtx.Tasks.Get(l.ctx, in.Id)
		if err != nil {
			return nil, rpcx.Err(err)
		}
		def, err := l.svcCtx.Registry.Load(l.ctx, t.TaskType, t.TypeVersion)
		if err != nil {
			return nil, rpcx.Err(err)
		}
		next, ok := def.Next(t.NodeID)
		if !ok {
			return nil, rpcx.Err(task.ErrState)
		}
		if ev.ExpectNode == "" {
			ev.ExpectNode = t.NodeID
		}
		if ev.Target, err = assign.Resolve(l.ctx, l.svcCtx.Relations, next, tasktype.DecodeValues(t.Fields)); err != nil {
			return nil, rpcx.Err(err)
		}
	default:
		return nil, rpcx.Invalid("action must be close, no_response, snooze or escalate")
	}
	t, err := l.svcCtx.Tasks.Do(l.ctx, in.Id, ev)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	s, err := rpcx.Summary(l.ctx, l.svcCtx, t, c)
	return s, rpcx.Err(err)
}
