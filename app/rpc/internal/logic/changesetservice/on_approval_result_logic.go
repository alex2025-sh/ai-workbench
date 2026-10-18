package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type OnApprovalResultLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewOnApprovalResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OnApprovalResultLogic {
	return &OnApprovalResultLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 公司审批平台回调：通过则原子发布，驳回则退回草稿
func (l *OnApprovalResultLogic) OnApprovalResult(in *pb.OnApprovalResultReq) (*pb.OnApprovalResultResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
