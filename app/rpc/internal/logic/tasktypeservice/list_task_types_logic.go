package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListTaskTypesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListTaskTypesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTaskTypesLogic {
	return &ListTaskTypesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 任务类型列表
func (l *ListTaskTypesLogic) ListTaskTypes(in *pb.ListTaskTypesReq) (*pb.ListTaskTypesResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	items, err := l.svcCtx.Registry.List(l.ctx, in.Keyword, in.Namespace)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	out := &pb.ListTaskTypesResp{}
	for _, s := range items {
		row := &pb.TaskTypeSummary{Key: s.Key, Namespace: s.Namespace, Name: s.Name, LatestVersion: int32(s.LatestVersion),
			HasDraft: s.HasDraft, UpdatedAt: s.UpdatedAt.UnixMilli()}
		if s.LatestVersion > 0 {
			if def, err := l.svcCtx.Registry.Load(l.ctx, s.Key, s.LatestVersion); err == nil {
				row.SourceKind = def.Source.Kind
			}
		}
		out.Items = append(out.Items, row)
	}
	return out, nil
}
