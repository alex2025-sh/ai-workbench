package taskservicelogic

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

func wantCode(t *testing.T, what string, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("%s: got %v, want %s", what, err, code)
	}
}

// 用 A1003：首次扫描后升级到 vip_cs_leads 组队列（lead、lead.carol），escalate 节点等待决定。
func TestTaskOperations(t *testing.T) {
	env := rpctest.New(t)
	id, _ := env.TaskID(t, "A1003")
	lead, carol, bob, alice, ops := rpctest.As("lead@example.com"), rpctest.As("lead.carol@example.com"),
		rpctest.As("rm.bob@example.com"), rpctest.As("rm.alice@example.com"), rpctest.As("ops@example.com")
	req := &pb.TaskIdReq{Id: id}

	// 认领：只有组成员能认领，只有一个人成功。
	_, err := NewClaimTaskLogic(bob, env.Svc).ClaimTask(req)
	wantCode(t, "outsider claims", err, codes.PermissionDenied)
	v, err := NewClaimTaskLogic(lead, env.Svc).ClaimTask(req)
	if err != nil || v.Status != pb.TaskStatus_TASK_STATUS_PENDING || v.AssigneeId != "lead@example.com" {
		t.Fatalf("lead claims: %+v %v", v, err)
	}
	_, err = NewClaimTaskLogic(carol, env.Svc).ClaimTask(req)
	wantCode(t, "second claim", err, codes.FailedPrecondition)
	if res, _ := NewListClaimableTasksLogic(carol, env.Svc).ListClaimableTasks(&pb.ListClaimableTasksReq{}); len(res.Items) != 0 {
		t.Fatalf("claimed task still claimable: %+v", res.Items)
	}
	d, _ := NewGetTaskLogic(carol, env.Svc).GetTask(req)
	if d.PendingAction.CanSignal {
		t.Fatal("other group member can still decide after the claim")
	}

	// 开始处理：只有处理人。
	_, err = NewStartProcessingLogic(carol, env.Svc).StartProcessing(req)
	wantCode(t, "non-assignee starts", err, codes.PermissionDenied)
	if v, err = NewStartProcessingLogic(lead, env.Svc).StartProcessing(req); err != nil || v.Status != pb.TaskStatus_TASK_STATUS_PROCESSING {
		t.Fatalf("lead starts: %+v %v", v, err)
	}
	_, err = NewStartProcessingLogic(lead, env.Svc).StartProcessing(req)
	wantCode(t, "start twice", err, codes.FailedPrecondition)

	// 引擎快照里仍是组队列：不重新分配的同步不能把任务退回待认领，也不能丢掉处理中。
	if _, err := env.Svc.Tasks.SyncStatus(context.Background(), task.SyncStatusCmd{TaskID: id, Status: task.TaskStatusUnassigned, Group: "vip_cs_leads", CurrentNode: "escalate", Seq: 1 << 40}); err != nil {
		t.Fatal(err)
	}
	if got, _ := env.Svc.Tasks.Get(context.Background(), id); got.Status != task.TaskStatusProcessing || got.AssigneeID != "lead@example.com" {
		t.Fatalf("sync reverted the claim: %+v", got)
	}

	// 调整优先级：处理人或 task.admin，原因必填。
	_, err = NewUpdatePriorityLogic(alice, env.Svc).UpdatePriority(&pb.UpdatePriorityReq{Id: id, Priority: pb.TaskPriority_TASK_PRIORITY_P0, Reason: "x"})
	wantCode(t, "outsider changes priority", err, codes.PermissionDenied)
	_, err = NewUpdatePriorityLogic(lead, env.Svc).UpdatePriority(&pb.UpdatePriorityReq{Id: id, Priority: pb.TaskPriority_TASK_PRIORITY_P0})
	wantCode(t, "no reason", err, codes.InvalidArgument)
	if v, err = NewUpdatePriorityLogic(lead, env.Svc).UpdatePriority(&pb.UpdatePriorityReq{Id: id, Priority: pb.TaskPriority_TASK_PRIORITY_P0, Reason: "客户投诉"}); err != nil || v.Priority != pb.TaskPriority_TASK_PRIORITY_P0 {
		t.Fatalf("priority: %+v %v", v, err)
	}

	// 转派：只改变处理人，状态不变；新处理人可以做决定。
	_, err = NewReassignLogic(bob, env.Svc).Reassign(&pb.ReassignReq{Id: id, AssigneeId: "rm.bob@example.com", Reason: "x"})
	wantCode(t, "outsider reassigns", err, codes.PermissionDenied)
	_, err = NewReassignLogic(ops, env.Svc).Reassign(&pb.ReassignReq{Id: id, Reason: "x"})
	wantCode(t, "no assignee", err, codes.InvalidArgument)
	if v, err = NewReassignLogic(ops, env.Svc).Reassign(&pb.ReassignReq{Id: id, AssigneeId: "rm.bob@example.com", Reason: "原 RM 接手"}); err != nil ||
		v.AssigneeId != "rm.bob@example.com" || v.Status != pb.TaskStatus_TASK_STATUS_PROCESSING {
		t.Fatalf("reassign: %+v %v", v, err)
	}
	if d, _ := NewGetTaskLogic(bob, env.Svc).GetTask(req); !d.PendingAction.CanSignal {
		t.Fatal("new assignee cannot decide")
	}

	evs, err := NewGetTaskEventsLogic(lead, env.Svc).GetTaskEvents(&pb.GetTaskEventsReq{Id: id})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, e := range evs.Items {
		seen[e.Type] = e.Actor
	}
	if seen["claimed"] != "lead@example.com" || seen["priority_changed"] != "lead@example.com" || seen["reassigned"] != "ops@example.com" {
		t.Fatalf("events = %v", seen)
	}

	// 不存在的任务。
	_, err = NewClaimTaskLogic(lead, env.Svc).ClaimTask(&pb.TaskIdReq{Id: 999999})
	wantCode(t, "missing task", err, codes.NotFound)
}
