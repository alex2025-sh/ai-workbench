package componentservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListDependentsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListDependentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDependentsLogic {
	return &ListDependentsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 查看哪些任务类型版本引用了该组件
func (l *ListDependentsLogic) ListDependents(in *pb.ListDependentsReq) (*pb.ListDependentsResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
