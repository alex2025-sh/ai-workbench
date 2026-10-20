package ingestservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/source"

	"github.com/zeromicro/go-zero/core/logx"
)

type PushLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPushLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PushLogic {
	return &PushLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *PushLogic) Push(in *pb.PushReq) (*pb.PushResp, error) {
	changed, err := l.svcCtx.Sources.Push(l.ctx, source.Envelope{TaskType: in.TaskType, Key: in.Key, Op: in.Op, OccurredAt: in.OccurredAt, Fields: in.FieldsJson})
	if err != nil {
		return nil, rpcx.Err(err)
	}
	return &pb.PushResp{Changed: changed}, nil
}
