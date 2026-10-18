package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AddDependentUpgradesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAddDependentUpgradesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddDependentUpgradesLogic {
	return &AddDependentUpgradesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 为共享组件的引用方一键生成升级草稿并加入变更集
func (l *AddDependentUpgradesLogic) AddDependentUpgrades(in *pb.AddDependentUpgradesReq) (*pb.ChangeSet, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
