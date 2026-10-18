package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DryRunTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDryRunTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DryRunTaskTypeDraftLogic {
	return &DryRunTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 试运行：动作节点只记录不执行，虚拟时钟
func (l *DryRunTaskTypeDraftLogic) DryRunTaskTypeDraft(in *pb.DryRunTaskTypeDraftReq) (*pb.DryRunResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
