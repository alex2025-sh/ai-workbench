package tasktypeservicelogic

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

func TestTaskTypeQueries(t *testing.T) {
	env := rpctest.New(t)
	ctx := rpctest.As("dev@example.com")

	res, err := NewListTaskTypesLogic(ctx, env.Svc).ListTaskTypes(&pb.ListTaskTypesReq{})
	if err != nil || len(res.Items) != 1 {
		t.Fatalf("list = %+v %v", res, err)
	}
	if s := res.Items[0]; s.Key != "vip_appeal_alert" || s.Name != "VIP 工单提醒" || s.Namespace != "cs_vip" || s.LatestVersion != 1 {
		t.Fatalf("summary = %+v", s)
	}
	for _, c := range []struct {
		req  *pb.ListTaskTypesReq
		want int
	}{
		{&pb.ListTaskTypesReq{Keyword: "VIP"}, 1},
		{&pb.ListTaskTypesReq{Keyword: "report"}, 0},
		{&pb.ListTaskTypesReq{Namespace: "cs_vip"}, 1},
		{&pb.ListTaskTypesReq{Namespace: "other"}, 0},
	} {
		if got, err := NewListTaskTypesLogic(ctx, env.Svc).ListTaskTypes(c.req); err != nil || len(got.Items) != c.want {
			t.Errorf("%+v: %d %v", c.req, len(got.GetItems()), err)
		}
	}

	got, err := NewGetTaskTypeLogic(ctx, env.Svc).GetTaskType(&pb.GetTaskTypeReq{Key: "vip_appeal_alert"})
	if err != nil || got.Version.Version != 1 || got.Version.ContentHash == "" {
		t.Fatalf("get = %+v %v", got, err)
	}
	if _, err := spec.Parse(got.BodyJson); err != nil {
		t.Fatalf("body is not a task type: %v", err)
	}
	// 编译后的绑定：notify_rm 沿用任务类型默认的 rm_card，escalate 用自己配置的 lead_card。
	var bindings map[string]struct {
		View     any    `json:"view"`
		ViewFrom string `json:"view_from"`
	}
	if err := json.Unmarshal(got.BindingsJson, &bindings); err != nil {
		t.Fatalf("bindings: %v", err)
	}
	if b := bindings["notify_rm"]; b.View != "rm_card" || b.ViewFrom != "task_type" {
		t.Errorf("notify_rm binding = %+v", b)
	}
	if b := bindings["escalate"]; b.View != "lead_card" || b.ViewFrom != "self" {
		t.Errorf("escalate binding = %+v", b)
	}
	for _, req := range []*pb.GetTaskTypeReq{{Key: "nope"}, {Key: "vip_appeal_alert", Version: 9}} {
		if _, err := NewGetTaskTypeLogic(ctx, env.Svc).GetTaskType(req); status.Code(err) != codes.NotFound {
			t.Errorf("%+v: %v", req, err)
		}
	}

	vs, err := NewListTaskTypeVersionsLogic(ctx, env.Svc).ListTaskTypeVersions(&pb.TaskTypeKeyReq{Key: "vip_appeal_alert"})
	if err != nil || len(vs.Items) != 1 || vs.Items[0].State != "published" {
		t.Fatalf("versions = %+v %v", vs, err)
	}
}
