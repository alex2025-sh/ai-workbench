package taskservicelogic

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
)

const (
	alice = "rm.alice@example.com"
	lead  = "lead@example.com"
	carol = "lead.carol@example.com"
	dave  = "reviewer.dave@example.com"
)

func list(t *testing.T, env *rpctest.Env, user, scope string) *pb.ListTasksResp {
	t.Helper()
	resp, err := NewListTasksLogic(rpctest.As(user), env.Svc).ListTasks(&pb.ListTasksReq{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func byTitle(items []*pb.TaskSummary, title string) *pb.TaskSummary {
	for _, it := range items {
		if it.Card.GetTitle() == title {
			return it
		}
	}
	return nil
}

func keys(as []*pb.ActionOption) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Key)
	}
	return out
}

func TestWorkbench(t *testing.T) {
	env := rpctest.New(t)

	mine := list(t, env, alice, "")
	if len(mine.Items) != 2 || len(mine.Counts) == 0 {
		t.Fatalf("alice mine = %d items, counts %v", len(mine.Items), mine.Counts)
	}
	a2 := byTitle(mine.Items, "VIP 工单已等待 48 小时：A1002")
	if a2 == nil || a2.TaskTypeName != "VIP 工单已等待 48 小时" || !a2.CanStart || a2.Card.NodeName != "RM 跟进" {
		t.Fatalf("A1002 summary = %+v", a2)
	}
	if got := keys(a2.Actions); len(got) != 4 || got[0] != "close" || got[3] != "escalate" {
		t.Fatalf("A1002 actions = %v", got)
	}
	for _, a := range a2.Actions {
		if a.Key == "escalate" && a.NextNodeName != "VIP 客服组长" {
			t.Errorf("escalate next = %q", a.NextNodeName)
		}
		if a.Key == "snooze" && len(a.SnoozeMs) != 3 {
			t.Errorf("snooze options = %v", a.SnoozeMs)
		}
		if a.Key == "link" && a.Url != "https://risk.example.com/appeal/A1002" {
			t.Errorf("link = %q", a.Url)
		}
	}

	// 队列：组长能看到认领队列与分配队列；分配队列里只有分配者能分配。
	q := list(t, env, lead, "queue")
	a3 := byTitle(q.Items, "VIP 工单超过 7 天未完成：A1003")
	a7 := byTitle(q.Items, "VIP 工单待审核：A1007")
	if a3 == nil || !a3.CanAssign || !a3.CanClaim || a7 == nil || !a7.CanClaim || a7.CanAssign {
		t.Fatalf("lead queue = %+v", q.Items)
	}
	if got := list(t, env, dave, "queue"); len(got.Items) != 0 {
		t.Fatalf("executor sees assign queue: %+v", got.Items)
	}
	d, err := NewGetTaskLogic(rpctest.As(lead), env.Svc).GetTask(&pb.TaskIdReq{Id: a3.Id})
	if err != nil || len(d.TeamMembers) != 3 || len(d.Nodes) != 1 || !d.Nodes[0].Current {
		t.Fatalf("A1003 detail = %+v %v", d, err)
	}
	if _, err := NewAssignTaskLogic(rpctest.As(lead), env.Svc).AssignTask(&pb.AssignTaskReq{Id: a3.Id, AssigneeId: dave}); err != nil {
		t.Fatal(err)
	}

	// 升级：A1002 交给组长团队；原 RM 只读可见，陌生人看不到。
	got, err := NewActTaskLogic(rpctest.As(alice), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "escalate", ExpectNode: "rm", Note: "RM 无法推动"})
	if err != nil || got.Status != pb.TaskStatus_TASK_STATUS_UNASSIGNED || got.NodeId != "lead" || got.TeamKey != "vip_cs_leads" {
		t.Fatalf("escalate = %+v %v", got, err)
	}
	if _, err := NewActTaskLogic(rpctest.As(alice), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "escalate", ExpectNode: "rm"}); status.Code(err) != codes.Aborted && status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("double escalate: %v", err)
	}
	ro, err := NewGetTaskLogic(rpctest.As(alice), env.Svc).GetTask(&pb.TaskIdReq{Id: a2.Id})
	if err != nil || !ro.ReadOnly || len(ro.Task.Actions) != 0 || len(ro.History) != 2 || !ro.Nodes[0].Visited || !ro.Nodes[1].Current {
		t.Fatalf("alice read-only detail = %+v %v", ro, err)
	}
	if _, err := NewGetTaskLogic(rpctest.As("stranger@example.com"), env.Svc).GetTask(&pb.TaskIdReq{Id: a2.Id}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stranger: %v", err)
	}
	involved := list(t, env, alice, "involved")
	if len(involved.Items) != 1 || involved.Items[0].Id != a2.Id {
		t.Fatalf("alice involved = %+v", involved.Items)
	}

	// 组员不认领直接处理：处理即认领。
	if _, err := NewActTaskLogic(rpctest.As(carol), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "snooze", SnoozeMs: int64(24 * time.Hour / time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewActTaskLogic(rpctest.As(carol), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "snooze", SnoozeMs: 1000}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("snooze not an option: %v", err)
	}
	done, err := NewActTaskLogic(rpctest.As(carol), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "no_response"})
	if err != nil || done.Status != pb.TaskStatus_TASK_STATUS_SOLVED || done.Resolution != "no_response" {
		t.Fatalf("no_response = %+v %v", done, err)
	}

	// 转派与优先级：处理人或 task.admin。
	a1 := byTitle(mine.Items, "VIP 工单待审核：A1001")
	if _, err := NewReassignLogic(rpctest.As(carol), env.Svc).Reassign(&pb.ReassignReq{Id: a1.Id, AssigneeId: carol, Reason: "x"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("reassign by other: %v", err)
	}
	if r, err := NewReassignLogic(rpctest.As("ops@example.com"), env.Svc).Reassign(&pb.ReassignReq{Id: a1.Id, AssigneeId: carol, Reason: "RM 休假"}); err != nil || r.AssigneeId != carol {
		t.Fatalf("admin reassign = %+v %v", r, err)
	}
	if r, err := NewUpdatePriorityLogic(rpctest.As(carol), env.Svc).UpdatePriority(&pb.UpdatePriorityReq{Id: a1.Id, Priority: pb.TaskPriority_TASK_PRIORITY_P0}); err != nil || r.Priority != pb.TaskPriority_TASK_PRIORITY_P0 {
		t.Fatalf("priority = %+v %v", r, err)
	}
	if r, err := NewStartProcessingLogic(rpctest.As(carol), env.Svc).StartProcessing(&pb.TaskIdReq{Id: a1.Id}); err != nil || r.Status != pb.TaskStatus_TASK_STATUS_PROCESSING {
		t.Fatalf("start = %+v %v", r, err)
	}
	if _, err := NewListTasksLogic(context.Background(), env.Svc).ListTasks(&pb.ListTasksReq{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous: %v", err)
	}
}

func TestListPaging(t *testing.T) {
	env := rpctest.New(t)
	seen := map[int64]bool{}
	cursor := ""
	for page := 0; page < 5; page++ {
		resp, err := NewListTasksLogic(rpctest.As(lead), env.Svc).ListTasks(&pb.ListTasksReq{Scope: "queue", Page: &pb.PageReq{Cursor: cursor, PageSize: 1}})
		if err != nil {
			t.Fatal(err)
		}
		if (page == 0) != (len(resp.Counts) > 0) {
			t.Fatalf("page %d counts = %v", page, resp.Counts)
		}
		for _, it := range resp.Items {
			if seen[it.Id] {
				t.Fatalf("duplicate %d", it.Id)
			}
			seen[it.Id] = true
		}
		if cursor = resp.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != 2 {
		t.Fatalf("paged %d tasks, want 2", len(seen))
	}
}
