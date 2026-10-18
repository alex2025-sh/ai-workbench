package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RetireVersionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRetireVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RetireVersionLogic {
	return &RetireVersionLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 下线某个版本；仍被引用的共享组件版本不能下线
func (l *RetireVersionLogic) RetireVersion(in *pb.RetireVersionReq) (*pb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
