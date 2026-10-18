package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"

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

// 对草稿及其依赖闭包完整编译校验
func (l *ValidateTaskTypeDraftLogic) ValidateTaskTypeDraft(in *pb.TaskTypeKeyReq) (*pb.ValidateResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	issues, err := l.svcCtx.TaskTypes.Validate(l.ctx, in.Key)
	if err != nil {
		return nil, draftError(err)
	}
	return &pb.ValidateResp{Ok: !compiler.HasErrors(issues), Issues: toIssues(issues)}, nil
}
