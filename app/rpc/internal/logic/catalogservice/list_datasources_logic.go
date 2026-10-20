package catalogservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListDatasourcesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListDatasourcesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDatasourcesLogic {
	return &ListDatasourcesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 平台注册的只读数据源连接
func (l *ListDatasourcesLogic) ListDatasources(in *pb.Empty) (*pb.NameList, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	return &pb.NameList{Names: l.svcCtx.Datasources.Names()}, nil
}
