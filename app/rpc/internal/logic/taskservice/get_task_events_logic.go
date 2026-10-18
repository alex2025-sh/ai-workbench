package taskservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"

	"github.com/zeromicro/go-zero/core/logx"
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
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	if _, err := loadTask(l.ctx, l.svcCtx, in.Id); err != nil {
		return nil, err
	}
	evs, next, err := l.svcCtx.Tasks.Events(l.ctx, in.Id, in.Page.GetCursor(), int(in.Page.GetPageSize()))
	if err != nil {
		return nil, queryError(err)
	}
	out := &pb.GetTaskEventsResp{NextCursor: next}
	for _, e := range evs {
		pe := &pb.TaskEvent{Id: e.ID, Type: e.Type, Actor: e.Actor, Reason: e.Reason, DetailJson: e.Detail, CreatedAt: e.CreatedAt.UnixMilli()}
		if e.From != nil {
			pe.FromStatus = pb.TaskStatus(task.StatusToProto(*e.From))
		}
		if e.To != nil {
			pe.ToStatus = pb.TaskStatus(task.StatusToProto(*e.To))
		}
		out.Items = append(out.Items, pe)
	}
	return out, nil
}
