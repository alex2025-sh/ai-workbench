package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTaskTypeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskTypeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskTypeLogic {
	return &GetTaskTypeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 已发布版本（version 为 0 取最新）与数据源状态
func (l *GetTaskTypeLogic) GetTaskType(in *pb.GetTaskTypeReq) (*pb.GetTaskTypeResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	s, err := l.svcCtx.Registry.Get(l.ctx, in.Key)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	v, err := l.svcCtx.Registry.Version(l.ctx, in.Key, int(in.Version))
	if err != nil {
		return nil, rpcx.Err(err)
	}
	out := &pb.GetTaskTypeResp{
		Summary: &pb.TaskTypeSummary{Key: s.Key, Namespace: s.Namespace, Name: s.Name, LatestVersion: int32(s.LatestVersion),
			HasDraft: s.HasDraft, UpdatedAt: s.UpdatedAt.UnixMilli()},
		Version:  &pb.VersionInfo{Version: int32(v.Version), ContentHash: v.ContentHash, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt.UnixMilli()},
		BodyJson: v.Body,
	}
	if def, err := tasktype.Parse(v.Body); err == nil {
		out.Summary.SourceKind = def.Source.Kind
		if def.Source.Kind == tasktype.SourceSQL {
			if st, err := model.NewSourceStateModel(l.svcCtx.Conn).FindOneByTaskType(l.ctx, in.Key); err == nil {
				out.SourceState = &pb.SourceState{NextRunAt: st.NextRunAt.UnixMilli(), LastRows: int32(st.LastRows),
					LastCreated: int32(st.LastCreated), LastClosed: int32(st.LastClosed), LastError: st.LastError}
				if st.LastRunAt.Valid {
					out.SourceState.LastRunAt = st.LastRunAt.Time.UnixMilli()
				}
			}
		}
	}
	return out, nil
}
