package runservicelogic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetRunLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRunLogic {
	return &GetRunLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 运行状态，供运行态画布着色
func (l *GetRunLogic) GetRun(in *pb.RunIdReq) (*pb.RunView, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	snap, err := l.svcCtx.Engine.Snapshot(l.ctx, in.RunId)
	if err != nil {
		return nil, queryError(err, fmt.Sprintf("run %d", in.RunId))
	}
	graph, _ := json.Marshal(snap.Def.Spec.Workflow)
	out := &pb.RunView{
		RunId: snap.Run.ID, TaskTypeKey: snap.Run.PlanKey, TaskTypeVersion: int32(snap.Run.PlanVersion),
		Status: string(snap.Run.Status), GraphJson: graph, UiJson: snap.Def.Spec.UI,
	}
	if b := snap.Blocked; b != nil {
		out.BlockedNode, out.BlockedReason = b.NodeID, b.Reason
	}
	for _, n := range snap.Nodes {
		out.Nodes = append(out.Nodes, &pb.NodeView{
			NodeId: n.NodeID, Iteration: int32(n.Iteration), Status: string(n.Status), Port: n.Port,
			Attempt: int32(n.Attempt), StartedAt: millis(n.StartedAt), FinishedAt: millis(n.FinishedAt),
			LastError: snap.Errors[n.NodeID],
		})
	}
	return out, nil
}

func millis(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.UnixMilli()
}
