package catalogservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetSkillSchemaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSkillSchemaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSkillSchemaLogic {
	return &GetSkillSchemaLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// skill 的输入输出 schema（转发 Skillhub）
func (l *GetSkillSchemaLogic) GetSkillSchema(in *pb.GetSkillSchemaReq) (*pb.GetSkillSchemaResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
