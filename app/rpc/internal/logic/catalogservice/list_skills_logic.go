package catalogservicelogic

import (
	"context"
	"sort"
	"strings"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListSkillsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListSkillsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSkillsLogic {
	return &ListSkillsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// skill 列表（转发 Skillhub）
func (l *ListSkillsLogic) ListSkills(in *pb.ListSkillsReq) (*pb.ListSkillsResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	// Skillhub 尚未接入：只列出平台内置 skill（都是只读查询）。
	var names []string
	for name := range l.svcCtx.Skills.Builtin {
		if in.Keyword == "" || strings.Contains(name, in.Keyword) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := &pb.ListSkillsResp{}
	for _, name := range names {
		if in.Category != "" && in.Category != "query" {
			continue
		}
		out.Items = append(out.Items, &pb.Skill{Name: name, DisplayName: name, Category: "query", Description: "平台内置 skill"})
	}
	return out, nil
}
