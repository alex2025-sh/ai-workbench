package tasktypeservicelogic

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

func TestTaskTypeLifecycle(t *testing.T) {
	env := rpctest.New(t)
	dev, ops := rpctest.As("dev@example.com"), rpctest.As("ops@example.com")

	list, err := NewListTaskTypesLogic(dev, env.Svc).ListTaskTypes(&pb.ListTaskTypesReq{Namespace: "cs_vip"})
	if err != nil || len(list.Items) != 3 || list.Items[0].SourceKind != "sql" {
		t.Fatalf("list = %+v %v", list, err)
	}
	got, err := NewGetTaskTypeLogic(dev, env.Svc).GetTaskType(&pb.GetTaskTypeReq{Key: "vip_appeal_7d"})
	if err != nil || got.Version.Version != 1 || got.SourceState == nil || got.SourceState.LastRows != 1 || got.SourceState.LastCreated != 1 {
		t.Fatalf("get = %+v %v", got, err)
	}

	// 新建 → 草稿有问题（候选人未填）→ 保存修正 → 发布需要权限。
	d, err := NewCreateTaskTypeLogic(dev, env.Svc).CreateTaskType(&pb.CreateTaskTypeReq{Key: "demo", Name: "演示", Namespace: "cs_vip"})
	if err != nil || len(d.Issues) == 0 {
		t.Fatalf("create = %+v %v", d, err)
	}
	if _, err := NewCreateTaskTypeLogic(dev, env.Svc).CreateTaskType(&pb.CreateTaskTypeReq{Key: "demo", Name: "x"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate create: %v", err)
	}
	tt, err := tasktype.Parse(d.BodyJson)
	if err != nil {
		t.Fatal(err)
	}
	tt.Nodes[0].Assignee.Users = []string{"lead@example.com"}
	tt.Source.Datasource, tt.Source.Query = "appeal_ro", "select appeal_id as `key`, appeal_type as title from appeal where appeal_status = 'COMMITED'"
	body, _ := json.Marshal(tt)
	saved, err := NewSaveTaskTypeDraftLogic(dev, env.Svc).SaveTaskTypeDraft(&pb.SaveTaskTypeDraftReq{Key: "demo", Revision: d.Revision, BodyJson: body})
	if err != nil || len(saved.Issues) != 0 {
		t.Fatalf("save = %+v %v", saved, err)
	}
	if _, err := NewSaveTaskTypeDraftLogic(dev, env.Svc).SaveTaskTypeDraft(&pb.SaveTaskTypeDraftReq{Key: "demo", Revision: d.Revision, BodyJson: body}); status.Code(err) != codes.Aborted {
		t.Fatalf("stale save: %v", err)
	}
	if _, err := NewPublishTaskTypeDraftLogic(dev, env.Svc).PublishTaskTypeDraft(&pb.TaskTypeKeyReq{Key: "demo"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("publish without permission: %v", err)
	}
	pub, err := NewPublishTaskTypeDraftLogic(ops, env.Svc).PublishTaskTypeDraft(&pb.TaskTypeKeyReq{Key: "demo"})
	if err != nil || pub.Version.GetVersion() != 1 {
		t.Fatalf("publish = %+v %v", pub, err)
	}

	// 草稿有问题时不发布，返回问题列表。
	d2, _ := NewGetTaskTypeDraftLogic(ops, env.Svc).GetTaskTypeDraft(&pb.TaskTypeKeyReq{Key: "demo"})
	tt, _ = tasktype.Parse(d2.BodyJson)
	tt.Nodes[0].Assignee = tasktype.Assignee{Kind: tasktype.AssigneeRelation, Relation: "nope", Field: "title", Fallback: "lead@example.com"}
	bad, _ := json.Marshal(tt)
	if _, err := NewSaveTaskTypeDraftLogic(ops, env.Svc).SaveTaskTypeDraft(&pb.SaveTaskTypeDraftReq{Key: "demo", Revision: d2.Revision, BodyJson: bad}); err != nil {
		t.Fatal(err)
	}
	pub, err = NewPublishTaskTypeDraftLogic(ops, env.Svc).PublishTaskTypeDraft(&pb.TaskTypeKeyReq{Key: "demo"})
	if err != nil || pub.Version != nil || len(pub.Issues) != 1 || pub.Issues[0].Path != "nodes.0.assignee.relation" {
		t.Fatalf("publish invalid = %+v %v", pub, err)
	}

	// 试跑 SQL 与卡片预览。
	trial, err := NewTrialSqlLogic(dev, env.Svc).TrialSql(&pb.TrialSqlReq{Datasource: "appeal_ro", Query: "select appeal_id as `key`, uid, order_amount from appeal order by appeal_id"})
	if err != nil || len(trial.Columns) != 2 {
		t.Fatalf("trial = %+v %v", trial, err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(trial.RowsJson, &rows); err != nil || len(rows) != 7 || rows[0]["key"] != "A1001" {
		t.Fatalf("rows = %v %v", rows, err)
	}
	if _, err := NewTrialSqlLogic(dev, env.Svc).TrialSql(&pb.TrialSqlReq{Datasource: "appeal_ro", Query: "delete from appeal"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("write query: %v", err)
	}
	v, _ := NewGetTaskTypeLogic(dev, env.Svc).GetTaskType(&pb.GetTaskTypeReq{Key: "vip_appeal_48h"})
	pc, err := NewPreviewCardLogic(dev, env.Svc).PreviewCard(&pb.PreviewCardReq{BodyJson: v.BodyJson, NodeId: "lead", Key: "A1",
		ValuesJson: []byte(`{"appeal_id":"A1","appeal_type":"withdraw_review","reviewer":"r","order_amount":"1.00","commit_time":"0"}`)})
	if err != nil || pc.Card.Title != "VIP 工单已等待 48 小时：A1" || pc.Card.NodeName != "VIP 客服组长" || len(pc.Card.Fields) != 4 {
		t.Fatalf("preview = %+v %v", pc, err)
	}
}
