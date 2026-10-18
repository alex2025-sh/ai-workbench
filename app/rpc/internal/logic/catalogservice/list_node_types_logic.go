package catalogservicelogic

import (
	"context"

	apischema "github.com/alex2025-sh/ai-workbench/api/schema"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"

	"github.com/zeromicro/go-zero/core/logx"
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
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	// 配置 schema 来自 api/schema/task_type.schema.json（与校验用的是同一份）；表单的中文标题写在 schema 的 title 中，
	// 控件由 $ref 的定义名决定（duration、expression、view_binding……），ui_schema 暂不需要。
	out := &pb.ListNodeTypesResp{}
	for _, t := range dsl.NodeTypes() {
		cfg, err := apischema.NodeConfigSchema(string(t.Type))
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, &pb.NodeType{
			Type: string(t.Type), Category: string(t.Category), DisplayName: t.DisplayName, Description: t.Description,
			ConfigSchemaJson: cfg, UiSchemaJson: []byte("{}"), PortRule: string(t.PortRule), FixedPorts: t.FixedPorts,
		})
	}
	return out, nil
}
