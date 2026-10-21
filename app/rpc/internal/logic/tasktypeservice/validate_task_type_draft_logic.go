package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ValidateTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewValidateTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ValidateTaskTypeDraftLogic {
	return &ValidateTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 完整校验草稿（含数据源连接、业务关系是否存在）
func (l *ValidateTaskTypeDraftLogic) ValidateTaskTypeDraft(in *pb.TaskTypeKeyReq) (*pb.ValidateResp, error) {
	user, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	d, err := l.svcCtx.Registry.GetDraft(l.ctx, in.Key, user)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	_, issues := l.svcCtx.Registry.Validate(l.ctx, d.Body)
	return &pb.ValidateResp{Issues: rpcx.Issues(issues)}, nil
}
