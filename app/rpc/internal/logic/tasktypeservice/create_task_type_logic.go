package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"regexp"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateTaskTypeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateTaskTypeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTaskTypeLogic {
	return &CreateTaskTypeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 新建任务类型并创建草稿
func (l *CreateTaskTypeLogic) CreateTaskType(in *pb.CreateTaskTypeReq) (*pb.DraftResp, error) {
	user, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`).MatchString(in.Key) || in.Name == "" {
		return nil, rpcx.Invalid("key must match ^[a-z][a-z0-9_]{0,63}$ and name is required")
	}
	d, err := l.svcCtx.Registry.Create(l.ctx, in.Key, in.Namespace, in.Name, in.Description, user)
	if err != nil {
		return nil, rpcx.Err(err)
	}
	_, issues := l.svcCtx.Registry.Validate(l.ctx, d.Body)
	return &pb.DraftResp{Key: d.Key, BaseVersion: int32(d.BaseVersion), Revision: d.Revision, BodyJson: d.Body, UpdatedBy: d.UpdatedBy,
		Issues: rpcx.Issues(issues)}, nil
}
