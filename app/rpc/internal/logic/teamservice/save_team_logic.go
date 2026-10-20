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

type SaveTeamLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveTeamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveTeamLogic {
	return &SaveTeamLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 新建或整体替换团队（含成员）；需要 team.admin 权限
func (l *SaveTeamLogic) SaveTeam(in *pb.Team) (*pb.Team, error) {
	user, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := identity.RequirePermission(l.ctx, l.svcCtx.Authz, user, identity.PermTeamAdmin); err != nil {
		return nil, err
	}
	t := &team.Team{Key: in.Key, Name: in.Name, ChatID: in.ChatId}
	for _, m := range in.Members {
		t.Members = append(t.Members, team.Member{UserID: m.UserId, Role: m.Role})
	}
	if err := team.Validate(t); err != nil {
		return nil, rpcx.Invalid("%v", err)
	}
	if err := l.svcCtx.Teams.Save(l.ctx, t, user); err != nil {
		return nil, rpcx.Err(err)
	}
	saved, err := l.svcCtx.Teams.Get(l.ctx, t.Key)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	return teamPB(saved), nil
}
