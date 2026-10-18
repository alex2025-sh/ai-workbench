package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTaskTypeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskTypeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskTypeLogic {
	return &GetTaskTypeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取已发布版本及其发布清单
func (l *GetTaskTypeLogic) GetTaskType(in *pb.GetTaskTypeReq) (*pb.GetTaskTypeResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	row, err := findPlan(l.ctx, l.svcCtx, in.Key, in.Version)
	if err != nil {
		return nil, err
	}
	latest, err := findPlan(l.ctx, l.svcCtx, in.Key, 0)
	if err != nil {
		return nil, err
	}
	sum, err := summary(latest)
	if err != nil {
		return nil, err
	}
	return &pb.GetTaskTypeResp{Summary: sum, Version: versionInfo(row), BodyJson: []byte(row.Dsl)}, nil
}
