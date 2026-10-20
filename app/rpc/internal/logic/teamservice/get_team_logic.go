package teamservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTeamLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTeamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTeamLogic {
	return &GetTeamLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetTeamLogic) GetTeam(in *pb.TeamKeyReq) (*pb.Team, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	t, err := l.svcCtx.Teams.Get(l.ctx, in.Key)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	return teamPB(t), nil
}
