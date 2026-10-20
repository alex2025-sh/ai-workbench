package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SaveTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveTaskTypeDraftLogic {
	return &SaveTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 保存草稿（revision 乐观锁），返回校验问题
func (l *SaveTaskTypeDraftLogic) SaveTaskTypeDraft(in *pb.SaveTaskTypeDraftReq) (*pb.SaveTaskTypeDraftResp, error) {
	user, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	rev, issues, err := l.svcCtx.Registry.SaveDraft(l.ctx, in.Key, in.Revision, in.BodyJson, user)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	return &pb.SaveTaskTypeDraftResp{Revision: rev, Issues: rpcx.Issues(issues)}, nil
}
