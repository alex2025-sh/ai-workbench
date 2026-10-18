package runservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
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
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	evs, next, err := l.svcCtx.Engine.RunEvents(l.ctx, in.RunId, in.Page.GetCursor(), int(in.Page.GetPageSize()))
	if err != nil {
		return nil, queryError(err, "run events")
	}
	out := &pb.GetRunEventsResp{NextCursor: next}
	for _, e := range evs {
		out.Items = append(out.Items, &pb.RunEvent{
			Id: e.ID, NodeId: e.NodeID, Iteration: int32(e.Iteration), Type: e.Type, PayloadJson: e.Payload,
			Actor: e.Actor, CreatedAt: e.CreatedAt.UnixMilli(),
		})
	}
	return out, nil
}
