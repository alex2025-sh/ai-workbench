package tasktypeservicelogic

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
)

func TestTaskTypeDraftLifecycle(t *testing.T) {
	env := rpctest.New(t)
	dev, ops, lead := rpctest.As("dev@example.com"), rpctest.As("ops@example.com"), rpctest.As("lead@example.com")
	code := func(err error) codes.Code { return status.Code(err) }

	// 新建：key 格式、必填项、重复。
	_, err := NewCreateTaskTypeLogic(dev, env.Svc).CreateTaskType(&pb.CreateTaskTypeReq{Key: "Bad-Key", Name: "x", Namespace: "demo"})
	if code(err) != codes.InvalidArgument {
		t.Fatalf("bad key: %v", err)
	}
	created, err := NewCreateTaskTypeLogic(dev, env.Svc).CreateTaskType(&pb.CreateTaskTypeReq{Key: "demo_new", Name: "新任务类型", Namespace: "demo"})
	if err != nil || created.Draft.BaseVersion != 0 || created.Draft.ContentHash == "" {
		t.Fatalf("create: %+v %v", created, err)
	}
	if _, err := NewCreateTaskTypeLogic(dev, env.Svc).CreateTaskType(&pb.CreateTaskTypeReq{Key: "vip_appeal_alert", Name: "x", Namespace: "demo"}); code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate: %v", err)
	}
	list, err := NewListTaskTypesLogic(dev, env.Svc).ListTaskTypes(&pb.ListTaskTypesReq{})
	if err != nil || len(list.Items) != 2 || list.Items[0].Key != "demo_new" || !list.Items[0].HasDraft || list.Items[0].LatestVersion != 0 {
		t.Fatalf("list = %+v %v", list, err)
	}

	// 保存：冲突与 key 不可改。
	draft := created.Draft
	if _, err := NewSaveTaskTypeDraftLogic(dev, env.Svc).SaveTaskTypeDraft(&pb.SaveTaskTypeDraftReq{Key: "demo_new", Revision: 9, BodyJson: draft.BodyJson}); code(err) != codes.Aborted {
		t.Fatalf("stale revision: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(draft.BodyJson, &body)
	body["key"] = "other"
	renamed, _ := json.Marshal(body)
	if _, err := NewSaveTaskTypeDraftLogic(dev, env.Svc).SaveTaskTypeDraft(&pb.SaveTaskTypeDraftReq{Key: "demo_new", Revision: draft.Revision, BodyJson: renamed}); code(err) != codes.InvalidArgument {
		t.Fatalf("key change: %v", err)
	}

	// 试运行草稿：开始 → 结束。
	dr, err := NewDryRunTaskTypeDraftLogic(dev, env.Svc).DryRunTaskTypeDraft(&pb.DryRunTaskTypeDraftReq{Key: "demo_new", SampleJson: [][]byte{[]byte(`{"title":"hello"}`)}})
	if err != nil || !dr.Passed || dr.Revision != draft.Revision || dr.ContentHash != draft.ContentHash || dr.Cases[0].EndStatus != "succeeded" {
		t.Fatalf("dry run: %+v %v", dr, err)
	}

	// 改坏：保存成功但带问题；校验、试运行、发布都报告问题。
	body["key"] = "demo_new"
	body["workflow"] = map[string]any{"nodes": []any{map[string]any{"id": "start", "type": "trigger"}}, "edges": []any{}}
	broken, _ := json.Marshal(body)
	saved, err := NewSaveTaskTypeDraftLogic(dev, env.Svc).SaveTaskTypeDraft(&pb.SaveTaskTypeDraftReq{Key: "demo_new", Revision: draft.Revision, BodyJson: broken})
	if err != nil || saved.Revision != draft.Revision+1 || len(saved.Issues) == 0 {
		t.Fatalf("save broken: %+v %v", saved, err)
	}
	if v, err := NewValidateTaskTypeDraftLogic(dev, env.Svc).ValidateTaskTypeDraft(&pb.TaskTypeKeyReq{Key: "demo_new"}); err != nil || v.Ok {
		t.Fatalf("validate: %+v %v", v, err)
	}
	if dr, err := NewDryRunTaskTypeDraftLogic(dev, env.Svc).DryRunTaskTypeDraft(&pb.DryRunTaskTypeDraftReq{Key: "demo_new", SampleJson: [][]byte{[]byte(`{}`)}}); err != nil || dr.Passed || len(dr.Issues) == 0 {
		t.Fatalf("dry run broken: %+v %v", dr, err)
	}

	// 发布：需要 task_type.publish；有错误时不发布；开关关闭时拒绝。
	publish := func(user string, rev int64) (*pb.PublishTaskTypeDraftResp, error) {
		return NewPublishTaskTypeDraftLogic(rpctest.As(user), env.Svc).PublishTaskTypeDraft(&pb.PublishTaskTypeDraftReq{Key: "demo_new", Revision: rev})
	}
	if _, err := publish("lead@example.com", saved.Revision); code(err) != codes.PermissionDenied {
		t.Fatalf("lead publishes: %v", err)
	}
	if res, err := publish("ops@example.com", saved.Revision); err != nil || res.Version != 0 || len(res.Issues) == 0 {
		t.Fatalf("publish broken: %+v %v", res, err)
	}
	fixed, err := NewSaveTaskTypeDraftLogic(dev, env.Svc).SaveTaskTypeDraft(&pb.SaveTaskTypeDraftReq{Key: "demo_new", Revision: saved.Revision, BodyJson: draft.BodyJson})
	if err != nil {
		t.Fatal(err)
	}
	env.Svc.Config.AllowDirectPublish = false
	if _, err := publish("ops@example.com", fixed.Revision); code(err) != codes.FailedPrecondition {
		t.Fatalf("publish disabled: %v", err)
	}
	env.Svc.Config.AllowDirectPublish = true
	res, err := publish("ops@example.com", fixed.Revision)
	if err != nil || res.Version != 1 {
		t.Fatalf("publish: %+v %v", res, err)
	}
	if got, err := NewGetTaskTypeLogic(ops, env.Svc).GetTaskType(&pb.GetTaskTypeReq{Key: "demo_new"}); err != nil || got.Version.Version != 1 {
		t.Fatalf("published: %+v %v", got, err)
	}

	// 已发布、没有草稿的任务类型：打开时基于最新版本创建草稿；丢弃后草稿消失。
	g, err := NewGetTaskTypeDraftLogic(lead, env.Svc).GetTaskTypeDraft(&pb.TaskTypeKeyReq{Key: "vip_appeal_alert"})
	if err != nil || g.Draft.BaseVersion != 1 {
		t.Fatalf("open published: %+v %v", g, err)
	}
	if _, err := NewDiscardTaskTypeDraftLogic(lead, env.Svc).DiscardTaskTypeDraft(&pb.DiscardDraftReq{Key: "vip_appeal_alert", Revision: g.Draft.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewValidateTaskTypeDraftLogic(lead, env.Svc).ValidateTaskTypeDraft(&pb.TaskTypeKeyReq{Key: "vip_appeal_alert"}); code(err) != codes.NotFound {
		t.Fatalf("validate without draft: %v", err)
	}
}
