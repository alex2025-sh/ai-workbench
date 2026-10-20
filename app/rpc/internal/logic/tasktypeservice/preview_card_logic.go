package tasktypeservicelogic

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpcx"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"

	"github.com/zeromicro/go-zero/core/logx"
)

type PreviewCardLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPreviewCardLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreviewCardLogic {
	return &PreviewCardLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 用一行样例数据渲染卡片（未保存的草稿也可以预览）
func (l *PreviewCardLogic) PreviewCard(in *pb.PreviewCardReq) (*pb.PreviewCardResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	def, issues := tasktype.Validate(in.BodyJson)
	if def == nil {
		var err error
		if def, err = tasktype.Parse(in.BodyJson); err != nil {
			return &pb.PreviewCardResp{Issues: rpcx.Issues(issues)}, nil
		}
	}
	node := in.NodeId
	if node == "" && len(def.Nodes) > 0 {
		node = def.Nodes[0].ID
	}
	card := def.RenderCard(node, in.Key, tasktype.DecodeValues(in.ValuesJson), "")
	return &pb.PreviewCardResp{Card: rpcx.CardPB(card), Issues: rpcx.Issues(issues)}, nil
}
