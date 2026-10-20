package teamservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/team"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListTeamsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListTeamsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTeamsLogic {
	return &ListTeamsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListTeamsLogic) ListTeams(in *pb.Empty) (*pb.ListTeamsResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	teams, err := l.svcCtx.Teams.List(l.ctx)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	out := &pb.ListTeamsResp{}
	for _, t := range teams {
		out.Items = append(out.Items, teamPB(t))
	}
	return out, nil
}

func teamPB(t *team.Team) *pb.Team {
	out := &pb.Team{Key: t.Key, Name: t.Name, ChatId: t.ChatID, UpdatedBy: t.UpdatedBy}
	for _, m := range t.Members {
		out.Members = append(out.Members, &pb.TeamMember{UserId: m.UserID, Role: m.Role})
	}
	return out
}
