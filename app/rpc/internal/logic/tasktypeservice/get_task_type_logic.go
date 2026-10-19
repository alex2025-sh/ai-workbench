package tasktypeservicelogic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"

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
	if _, err := l.svcCtx.TaskTypes.Find(l.ctx, in.Key); err == nil {
		sum.HasDraft = true
	}
	var plan compiler.Plan
	if err := json.Unmarshal([]byte(row.Compiled), &plan); err != nil {
		return nil, fmt.Errorf("task type %s@%d: compiled plan: %w", row.PlanId, row.Version, err)
	}
	bindings, err := json.Marshal(plan.Bindings)
	if err != nil {
		return nil, err
	}
	return &pb.GetTaskTypeResp{Summary: sum, Version: versionInfo(row), BodyJson: []byte(row.Dsl), BindingsJson: bindings}, nil
}
