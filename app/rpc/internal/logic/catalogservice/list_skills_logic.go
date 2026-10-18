package catalogservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
