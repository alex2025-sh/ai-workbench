package tasktypeservicelogic

import (
	"context"
	"strings"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
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

// 任务类型列表，按 namespace 筛选
func (l *ListTaskTypesLogic) ListTaskTypes(in *pb.ListTaskTypesReq) (*pb.ListTaskTypesResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	entries, err := l.svcCtx.TaskTypes.List(l.ctx)
	if err != nil {
		return nil, err
	}
	// 任务类型数量有限，按 namespace 与关键词在内存中过滤，一次返回全部（next_cursor 为空）。
	out := &pb.ListTaskTypesResp{}
	kw := strings.ToLower(in.Keyword)
	for _, e := range entries {
		if in.Namespace != "" && e.Namespace != in.Namespace {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(e.Key), kw) && !strings.Contains(strings.ToLower(e.Name), kw) {
			continue
		}
		out.Items = append(out.Items, &pb.TaskTypeSummary{
			Key: e.Key, Name: e.Name, Namespace: e.Namespace, LatestVersion: int32(e.LatestVersion), HasDraft: e.HasDraft,
			Status: "active", UpdatedAt: e.UpdatedAt.UnixMilli(),
		})
	}
	return out, nil
}
