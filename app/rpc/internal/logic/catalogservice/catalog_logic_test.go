package catalogservicelogic

import (
	"encoding/json"
	"testing"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
)

func TestCatalog(t *testing.T) {
	env := rpctest.New(t)
	ctx := rpctest.As("dev@example.com")
	res, err := NewListNodeTypesLogic(ctx, env.Svc).ListNodeTypes(&pb.Empty{})
	if err != nil || len(res.Items) != 11 {
		t.Fatalf("node types: %d %v", len(res.GetItems()), err)
	}
	for _, n := range res.Items {
		var s map[string]any
		if err := json.Unmarshal(n.ConfigSchemaJson, &s); err != nil || s["$defs"] == nil {
			t.Fatalf("%s schema: %v", n.Type, err)
		}
		if n.Type == "human_choice" && n.PortRule != "from_options" {
			t.Fatalf("human_choice port rule = %s", n.PortRule)
		}
	}
	skills, err := NewListSkillsLogic(ctx, env.Svc).ListSkills(&pb.ListSkillsReq{Keyword: "appeal"})
	if err != nil || len(skills.Items) == 0 || skills.Items[0].Name != "appeal.get_status" {
		t.Fatalf("skills: %+v %v", skills, err)
	}
}
