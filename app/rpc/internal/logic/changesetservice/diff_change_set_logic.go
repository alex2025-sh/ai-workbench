package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DiffChangeSetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDiffChangeSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DiffChangeSetLogic {
	return &DiffChangeSetLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 变更集内各项与其基础版本的差异（审批单链接到此）
func (l *DiffChangeSetLogic) DiffChangeSet(in *pb.ChangeSetIdReq) (*pb.DiffChangeSetResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
