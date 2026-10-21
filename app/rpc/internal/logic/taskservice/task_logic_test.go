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
	erin  = "reviewer.erin@example.com"
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
	if got := keys(a2.Actions); len(got) != 4 || got[0] != "close" || got[3] != "escalate" || !a2.CanTransfer {
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

	// 业务关系查不到 → 兜底人。
	a7 := byTitle(list(t, env, lead, "").Items, "VIP 工单待审核：A1007")
	if a7 == nil || a7.AssigneeId != lead || !a7.CanStart {
		t.Fatalf("A1007 (fallback) = %+v", a7)
	}
	// 认领节点：待认领所有人可见、可认领；认领前没有节点动作，先到先得。
	a3 := byTitle(list(t, env, "stranger@example.com", "claimable").Items, "VIP 工单超过 7 天未完成：A1003")
	if a3 == nil || a3.Status != pb.TaskStatus_TASK_STATUS_UNASSIGNED || !a3.CanClaim || !a3.ClaimNode || a3.CanTransfer || len(keys(a3.Actions)) != 1 {
		t.Fatalf("A1003 (claim) = %+v", a3)
	}
	d, err := NewGetTaskLogic(rpctest.As(dave), env.Svc).GetTask(&pb.TaskIdReq{Id: a3.Id})
	if err != nil || d.ReadOnly || len(d.Nodes) != 1 || !d.Nodes[0].Current || d.Nodes[0].Assignee != "通知到群 oc_vip_review 认领，2h 无人认领再提醒一次" {
		t.Fatalf("A1003 detail = %+v %v", d, err)
	}
	if c, err := NewClaimTaskLogic(rpctest.As(dave), env.Svc).ClaimTask(&pb.TaskIdReq{Id: a3.Id}); err != nil || c.AssigneeId != dave || !c.CanTransfer || len(c.Actions) != 4 {
		t.Fatalf("claim = %+v %v", c, err)
	}
	if _, err := NewClaimTaskLogic(rpctest.As(erin), env.Svc).ClaimTask(&pb.TaskIdReq{Id: a3.Id}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second claim: %v", err)
	}
	// 转交：必须写原因；放回待认领后 erin 认领，详情里记转交次数。
	if _, err := NewTransferTaskLogic(rpctest.As(dave), env.Svc).TransferTask(&pb.TransferTaskReq{Id: a3.Id}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("transfer without reason: %v", err)
	}
	if r, err := NewTransferTaskLogic(rpctest.As(dave), env.Svc).TransferTask(&pb.TransferTaskReq{Id: a3.Id, Reason: "不熟悉", ExpectNode: "review"}); err != nil || r.Status != pb.TaskStatus_TASK_STATUS_UNASSIGNED {
		t.Fatalf("release = %+v %v", r, err)
	}
	if _, err := NewClaimTaskLogic(rpctest.As(erin), env.Svc).ClaimTask(&pb.TaskIdReq{Id: a3.Id}); err != nil {
		t.Fatal(err)
	}
	if d, err := NewGetTaskLogic(rpctest.As(dave), env.Svc).GetTask(&pb.TaskIdReq{Id: a3.Id}); err != nil || !d.ReadOnly || d.TransferCount != 1 {
		t.Fatalf("dave after release = %+v %v", d, err)
	}

	// 升级：A1002 随机交给一位组长；原 RM 只读可见，陌生人看不到。
	got, err := NewActTaskLogic(rpctest.As(alice), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "escalate", ExpectNode: "rm", Note: "RM 无法推动"})
	if err != nil || got.Status != pb.TaskStatus_TASK_STATUS_PENDING || got.NodeId != "lead" || (got.AssigneeId != lead && got.AssigneeId != carol) {
		t.Fatalf("escalate = %+v %v", got, err)
	}
	leader := got.AssigneeId
	if _, err := NewActTaskLogic(rpctest.As(alice), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "escalate", ExpectNode: "rm"}); status.Code(err) != codes.Aborted && status.Code(err) != codes.FailedPrecondition && status.Code(err) != codes.PermissionDenied {
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

	// 只有当前处理人能处理。
	other := lead
	if leader == lead {
		other = carol
	}
	if _, err := NewActTaskLogic(rpctest.As(other), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "no_response"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-assignee act: %v", err)
	}
	if _, err := NewActTaskLogic(rpctest.As(leader), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "snooze", SnoozeMs: int64(24 * time.Hour / time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewActTaskLogic(rpctest.As(leader), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "snooze", SnoozeMs: 1000}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("snooze not an option: %v", err)
	}
	done, err := NewActTaskLogic(rpctest.As(leader), env.Svc).ActTask(&pb.ActTaskReq{Id: a2.Id, Action: "no_response"})
	if err != nil || done.Status != pb.TaskStatus_TASK_STATUS_SOLVED || done.Resolution != "no_response" {
		t.Fatalf("no_response = %+v %v", done, err)
	}

	// 转交与优先级：处理人或 task.admin。
	a1 := byTitle(mine.Items, "VIP 工单待审核：A1001")
	if !a1.CanTransfer || a1.ClaimNode {
		t.Fatalf("A1001 transfer flags = %+v", a1)
	}
	if _, err := NewTransferTaskLogic(rpctest.As(carol), env.Svc).TransferTask(&pb.TransferTaskReq{Id: a1.Id, AssigneeId: carol, Reason: "x"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("transfer by other: %v", err)
	}
	if _, err := NewTransferTaskLogic(rpctest.As(alice), env.Svc).TransferTask(&pb.TransferTaskReq{Id: a1.Id, Reason: "x"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("release on a non-claim node: %v", err)
	}
	if r, err := NewTransferTaskLogic(rpctest.As("ops@example.com"), env.Svc).TransferTask(&pb.TransferTaskReq{Id: a1.Id, AssigneeId: carol, Reason: "RM 休假"}); err != nil || r.AssigneeId != carol {
		t.Fatalf("admin transfer = %+v %v", r, err)
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
		resp, err := NewListTasksLogic(rpctest.As(alice), env.Svc).ListTasks(&pb.ListTasksReq{Page: &pb.PageReq{Cursor: cursor, PageSize: 1}})
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
