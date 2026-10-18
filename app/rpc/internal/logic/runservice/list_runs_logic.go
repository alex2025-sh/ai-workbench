package runservicelogic

import (
	"context"
	"errors"
	"time"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListRunsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListRunsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRunsLogic {
	return &ListRunsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 按任务、biz_key、状态查运行
func (l *ListRunsLogic) ListRuns(in *pb.ListRunsReq) (*pb.ListRunsResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	q := engine.RunQuery{Status: engine.RunStatus(in.Status), PlanKey: in.TaskTypeKey, Cursor: in.Page.GetCursor(), PageSize: int(in.Page.GetPageSize())}
	if in.CreatedAfter > 0 {
		q.CreatedAfter = time.UnixMilli(in.CreatedAfter).UTC()
	}
	if in.CreatedBefore > 0 {
		q.CreatedBefore = time.UnixMilli(in.CreatedBefore).UTC()
	}
	// 按任务或 biz_key 查询时先解析出 run；找不到就是空列表。
	if in.TaskId != 0 {
		t, err := l.svcCtx.Tasks.Get(l.ctx, in.TaskId)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
		if t == nil || t.RunID == 0 {
			return &pb.ListRunsResp{}, nil
		}
		q.IDs = append(q.IDs, t.RunID)
	}
	if in.BizKey != "" {
		r, err := l.svcCtx.Engine.RunByBizKey(l.ctx, in.BizKey)
		if err != nil {
			return nil, err
		}
		if r == nil {
			return &pb.ListRunsResp{}, nil
		}
		q.IDs = append(q.IDs, r.ID)
	}
	runs, next, err := l.svcCtx.Engine.ListRuns(l.ctx, q)
	if err != nil {
		return nil, queryError(err, "runs")
	}
	out := &pb.ListRunsResp{NextCursor: next}
	for _, r := range runs {
		item := &pb.RunSummary{
			RunId: r.Run.ID, TaskTypeKey: r.Run.PlanKey, TaskTypeVersion: int32(r.Run.PlanVersion), Status: string(r.Run.Status),
			CreatedAt: r.CreatedAt.UnixMilli(), UpdatedAt: r.UpdatedAt.UnixMilli(),
		}
		if b := r.Blocked; b != nil {
			item.BlockedNode, item.BlockedReason = b.NodeID, b.Reason
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
