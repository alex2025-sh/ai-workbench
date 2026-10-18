package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetChangeSetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetChangeSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetChangeSetLogic {
	return &GetChangeSetLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 变更集详情
func (l *GetChangeSetLogic) GetChangeSet(in *pb.ChangeSetIdReq) (*pb.ChangeSet, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
