package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateChangeSetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateChangeSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateChangeSetLogic {
	return &CreateChangeSetLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 把草稿打包为变更集，并对依赖闭包编译校验
func (l *CreateChangeSetLogic) CreateChangeSet(in *pb.CreateChangeSetReq) (*pb.ChangeSet, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
