package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListTaskTypeVersionsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListTaskTypeVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTaskTypeVersionsLogic {
	return &ListTaskTypeVersionsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 版本历史
func (l *ListTaskTypeVersionsLogic) ListTaskTypeVersions(in *pb.TaskTypeKeyReq) (*pb.ListVersionsResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	rows, err := plans(l.svcCtx).Versions(l.ctx, in.Key)
	if err != nil {
		return nil, err
	}
	out := &pb.ListVersionsResp{}
	for _, row := range rows {
		out.Items = append(out.Items, versionInfo(row))
	}
	return out, nil
}
