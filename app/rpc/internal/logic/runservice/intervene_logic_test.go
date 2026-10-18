package runservicelogic

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

// 重试与跳过的语义由引擎测试覆盖（internal/flow/engine/admin_test.go），这里验证权限、参数与错误码。
// A1003 在 escalate 人工节点等待：run 运行中，可以跳过、不能重试。
func TestIntervention(t *testing.T) {
	env := rpctest.New(t)
	taskID, runID := env.TaskID(t, "A1003")
	skip := func(user, node, port, reason string) error {
		_, err := NewSkipNodeLogic(rpctest.As(user), env.Svc).SkipNode(&pb.AdminNodeReq{RunId: runID, NodeId: node, Port: port, Reason: reason})
		return err
	}
	retry := func(user, node, reason string) error {
		_, err := NewRetryNodeLogic(rpctest.As(user), env.Svc).RetryNode(&pb.AdminNodeReq{RunId: runID, NodeId: node, Reason: reason})
		return err
	}
	for _, c := range []struct {
		what string
		err  error
		code codes.Code
	}{
		{"lead has no run.admin", skip("lead@example.com", "escalate", "handled", "x"), codes.PermissionDenied},
		{"reason required", skip("ops@example.com", "escalate", "handled", ""), codes.InvalidArgument},
		{"unknown node", skip("ops@example.com", "nope", "handled", "x"), codes.InvalidArgument},
		{"invalid port", skip("ops@example.com", "escalate", "nope", "x"), codes.InvalidArgument},
		{"skip a concluded node", skip("ops@example.com", "notify_rm", "next", "x"), codes.FailedPrecondition},
		{"retry a running run", retry("ops@example.com", "escalate", "x"), codes.FailedPrecondition},
	} {
		if status.Code(c.err) != c.code {
			t.Fatalf("%s: %v, want %s", c.what, c.err, c.code)
		}
	}
	if err := skip("ops@example.com", "escalate", "handled", "组长离职，按已处理结束"); err != nil {
		t.Fatal(err)
	}
	v, err := NewGetRunLogic(rpctest.As("ops@example.com"), env.Svc).GetRun(&pb.RunIdReq{RunId: runID})
	if err != nil || v.Status != "succeeded" {
		t.Fatalf("run = %+v %v", v, err)
	}
	if tk, _ := env.Svc.Tasks.Get(context.Background(), taskID); tk.Status != task.TaskStatusSolved {
		t.Fatalf("task = %+v", tk)
	}
}
