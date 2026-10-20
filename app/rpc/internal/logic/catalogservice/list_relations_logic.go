package catalogservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"sort"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListRelationsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListRelationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRelationsLogic {
	return &ListRelationsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 可用的业务关系（如 vip_rm）
func (l *ListRelationsLogic) ListRelations(in *pb.Empty) (*pb.NameList, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	names := l.svcCtx.Relations.Names()
	sort.Strings(names)
	return &pb.NameList{Names: names}, nil
}
