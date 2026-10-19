package taskservicelogic

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
)

func titles(items []*pb.TaskSummary) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func TestTaskQueries(t *testing.T) {
	env := rpctest.New(t)
	alice, lead, bob := rpctest.As("rm.alice@example.com"), rpctest.As("lead@example.com"), rpctest.As("rm.bob@example.com")

	t.Run("my tasks paginate by priority, due and id", func(t *testing.T) {
		page := func(cursor string) *pb.ListMyTasksResp {
			res, err := NewListMyTasksLogic(alice, env.Svc).ListMyTasks(&pb.ListMyTasksReq{Page: &pb.PageReq{Cursor: cursor, PageSize: 1}})
			if err != nil {
				t.Fatal(err)
			}
			return res
		}
		first := page("")
		if got := titles(first.Items); len(got) != 1 || got[0] != "VIP 工单待审核：A1001" || first.NextCursor == "" {
			t.Fatalf("page 1 = %v next=%q", got, first.NextCursor)
		}
		second := page(first.NextCursor)
		// 标题取最近发出的卡片：A1002 已发过 48 小时提醒。
		if got := titles(second.Items); len(got) != 1 || got[0] != "VIP 工单已等待 48 小时：A1002" || second.NextCursor != "" {
			t.Fatalf("page 2 = %v next=%q", got, second.NextCursor)
		}
		if first.Items[0].Subtitle == "" || first.Items[0].Status != pb.TaskStatus_TASK_STATUS_PENDING {
			t.Fatalf("summary = %+v", first.Items[0])
		}
		if first.Items[0].TaskTypeName != "VIP 工单提醒" || first.Items[0].PendingAction != nil {
			t.Fatalf("summary type name / pending = %q %+v", first.Items[0].TaskTypeName, first.Items[0].PendingAction)
		}
		// 计数只在第一页返回，覆盖全部结果而不是这一页：alice 有 2 个 P1 待处理任务。
		if len(first.Counts) != 1 || first.Counts[0].Priority != pb.TaskPriority_TASK_PRIORITY_P1 ||
			first.Counts[0].Status != pb.TaskStatus_TASK_STATUS_PENDING || first.Counts[0].Count != 2 {
			t.Fatalf("counts = %+v", first.Counts)
		}
		if len(second.Counts) != 0 {
			t.Fatalf("counts on page 2 = %+v", second.Counts)
		}
	})

	t.Run("counts follow the filters", func(t *testing.T) {
		res, err := NewListMyTasksLogic(alice, env.Svc).ListMyTasks(&pb.ListMyTasksReq{Priorities: []pb.TaskPriority{pb.TaskPriority_TASK_PRIORITY_P0}})
		if err != nil || len(res.Items) != 0 || len(res.Counts) != 0 {
			t.Fatalf("P0 only = %+v %v", res, err)
		}
	})

	t.Run("bad cursor", func(t *testing.T) {
		_, err := NewListMyTasksLogic(alice, env.Svc).ListMyTasks(&pb.ListMyTasksReq{Page: &pb.PageReq{Cursor: "nope"}})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("claimable is the caller's group queue", func(t *testing.T) {
		res, err := NewListClaimableTasksLogic(lead, env.Svc).ListClaimableTasks(&pb.ListClaimableTasksReq{})
		if err != nil {
			t.Fatal(err)
		}
		if got := titles(res.Items); len(got) != 1 || got[0] != "VIP 工单超过 7 天未完成：A1003" {
			t.Fatalf("lead claimable = %v", got)
		}
		// 组任务不需要先认领就能做决定：列表卡片带上可直接点击的操作。
		if p := res.Items[0].PendingAction; p == nil || p.NodeId != "escalate" || !p.CanSignal || len(p.Options) != 2 {
			t.Fatalf("claimable pending = %+v", p)
		}
		if len(res.Counts) != 1 || res.Counts[0].Status != pb.TaskStatus_TASK_STATUS_UNASSIGNED || res.Counts[0].Count != 1 {
			t.Fatalf("claimable counts = %+v", res.Counts)
		}
		res, err = NewListClaimableTasksLogic(alice, env.Svc).ListClaimableTasks(&pb.ListClaimableTasksReq{})
		if err != nil || len(res.Items) != 0 {
			t.Fatalf("alice claimable = %v %v", res, err)
		}
	})

	t.Run("detail of a queued task", func(t *testing.T) {
		id, _ := env.TaskID(t, "A1003")
		d, err := NewGetTaskLogic(lead, env.Svc).GetTask(&pb.TaskIdReq{Id: id})
		if err != nil {
			t.Fatal(err)
		}
		p := d.PendingAction
		if p == nil || p.NodeId != "escalate" || !p.CanSignal || len(p.Options) != 2 {
			t.Fatalf("pending = %+v", p)
		}
		if p.Options[0].Key != "handled" || p.Options[1].SnoozeMs != 24*3600*1000 {
			t.Fatalf("options = %+v", p.Options)
		}
		var card action.Card
		if err := json.Unmarshal(d.RenderedJson, &card); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(card.Title, "超过 7 天") || len(card.Actions) != 0 {
			t.Fatalf("card = %+v", card)
		}
		// 卡片用节点当时的环境渲染：原 RM 来自 task.assignee（升级前的处理人）。
		var rm string
		for _, f := range card.Fields {
			if f.Label == "原 RM" {
				rm = f.Value
			}
		}
		if rm != "rm.bob@example.com" {
			t.Fatalf("原 RM = %q", rm)
		}
		var payload map[string]any
		if err := json.Unmarshal(d.Task.PayloadJson, &payload); err != nil || !strings.Contains(payload["uid"].(string), "*") {
			t.Fatalf("payload not masked: %s", d.Task.PayloadJson)
		}
		steps := map[string]string{}
		for _, s := range d.Progress {
			steps[s.NodeId] = s.Status
		}
		if steps["notify_rm"] != "succeeded" || steps["escalate"] != "waiting" || steps["start"] != "" {
			t.Fatalf("progress = %v", steps)
		}

		d, err = NewGetTaskLogic(bob, env.Svc).GetTask(&pb.TaskIdReq{Id: id})
		if err != nil || d.PendingAction == nil || d.PendingAction.CanSignal {
			t.Fatalf("outsider must see the action read-only: %+v %v", d.GetPendingAction(), err)
		}
	})

	t.Run("detail without a waiting human node", func(t *testing.T) {
		id, _ := env.TaskID(t, "A1001")
		d, err := NewGetTaskLogic(alice, env.Svc).GetTask(&pb.TaskIdReq{Id: id})
		if err != nil {
			t.Fatal(err)
		}
		var card action.Card
		_ = json.Unmarshal(d.RenderedJson, &card)
		if d.PendingAction != nil || card.Title != "VIP 工单待审核：A1001" {
			t.Fatalf("pending=%+v card=%+v", d.PendingAction, card)
		}
	})

	t.Run("detail keeps the sent card after the decision", func(t *testing.T) {
		id, runID := env.TaskID(t, "A1003")
		res, err := env.Svc.Engine.Signal(lead, engine.SignalCmd{RunID: runID, NodeID: "escalate", Option: "handled", Actor: "lead@example.com"})
		if err != nil || !res.Accepted {
			t.Fatalf("signal: %+v %v", res, err)
		}
		d, err := NewGetTaskLogic(lead, env.Svc).GetTask(&pb.TaskIdReq{Id: id})
		if err != nil {
			t.Fatal(err)
		}
		var card action.Card
		_ = json.Unmarshal(d.RenderedJson, &card)
		if d.PendingAction != nil || !strings.Contains(card.Title, "超过 7 天") || d.Task.Status != pb.TaskStatus_TASK_STATUS_SOLVED {
			t.Fatalf("pending=%+v card=%+v status=%v", d.PendingAction, card, d.Task.Status)
		}
		for _, f := range card.Fields {
			if f.Label == "原 RM" && f.Value != "rm.bob@example.com" {
				t.Fatalf("原 RM = %q", f.Value)
			}
		}
	})

	t.Run("events and errors", func(t *testing.T) {
		id, _ := env.TaskID(t, "A1003")
		res, err := NewGetTaskEventsLogic(lead, env.Svc).GetTaskEvents(&pb.GetTaskEventsReq{Id: id})
		if err != nil || len(res.Items) == 0 {
			t.Fatalf("events = %+v %v", res, err)
		}
		if _, err := NewGetTaskLogic(lead, env.Svc).GetTask(&pb.TaskIdReq{Id: 999999}); status.Code(err) != codes.NotFound {
			t.Fatalf("missing task: %v", err)
		}
		if _, err := NewGetTaskLogic(rpctest.As(""), env.Svc).GetTask(&pb.TaskIdReq{Id: id}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("anonymous: %v", err)
		}
	})
}
