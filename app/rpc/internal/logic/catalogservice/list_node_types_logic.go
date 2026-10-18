package catalogservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListNodeTypesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListNodeTypesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListNodeTypesLogic {
	return &ListNodeTypesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 节点类型目录：配置 schema、UI 提示、端口规则
func (l *ListNodeTypesLogic) ListNodeTypes(in *pb.Empty) (*pb.ListNodeTypesResp, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
