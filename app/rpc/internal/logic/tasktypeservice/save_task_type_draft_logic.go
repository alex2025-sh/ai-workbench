package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
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

// 保存草稿（revision 乐观锁）
func (l *SaveTaskTypeDraftLogic) SaveTaskTypeDraft(in *pb.SaveTaskTypeDraftReq) (*pb.SaveTaskTypeDraftResp, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	d, issues, err := l.svcCtx.TaskTypes.Save(l.ctx, in.Key, in.Revision, in.BodyJson, actor)
	if err != nil {
		return nil, draftError(err)
	}
	return &pb.SaveTaskTypeDraftResp{Revision: d.Revision, Issues: toIssues(issues)}, nil
}
