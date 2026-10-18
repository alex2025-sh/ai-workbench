package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WithdrawChangeSetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewWithdrawChangeSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WithdrawChangeSetLogic {
	return &WithdrawChangeSetLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 撤回审批，草稿解锁
func (l *WithdrawChangeSetLogic) WithdrawChangeSet(in *pb.ChangeSetIdReq) (*pb.ChangeSet, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
