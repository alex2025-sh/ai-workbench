package changesetservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SubmitChangeSetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSubmitChangeSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubmitChangeSetLogic {
	return &SubmitChangeSetLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 提交审批：快照草稿、锁定、在公司审批平台创建审批实例
func (l *SubmitChangeSetLogic) SubmitChangeSet(in *pb.ChangeSetIdReq) (*pb.SubmitChangeSetResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
