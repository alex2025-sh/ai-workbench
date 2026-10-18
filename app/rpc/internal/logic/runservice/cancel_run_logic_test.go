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

func TestCancelRun(t *testing.T) {
	env := rpctest.New(t)
	taskID, runID := env.TaskID(t, "A1001")
	cancel := func(user, reason string, run int64) error {
		_, err := NewCancelRunLogic(rpctest.As(user), env.Svc).CancelRun(&pb.CancelRunReq{RunId: run, Reason: reason})
		return err
	}
	for _, c := range []struct {
		user, reason string
		run          int64
		code         codes.Code
	}{
		{"rm.alice@example.com", "x", runID, codes.PermissionDenied}, // 处理人也不能取消运行，需要 run.admin
		{"ops@example.com", " ", runID, codes.InvalidArgument},
		{"ops@example.com", "x", 999999, codes.NotFound},
		{"ops@example.com", "工单已线下处理", runID, codes.OK},
		{"ops@example.com", "重复取消", runID, codes.OK}, // 已结束的 run 不受影响
	} {
		if err := cancel(c.user, c.reason, c.run); status.Code(err) != c.code {
			t.Fatalf("%s cancels run %d: %v, want %s", c.user, c.run, err, c.code)
		}
	}
	v, err := NewGetRunLogic(rpctest.As("ops@example.com"), env.Svc).GetRun(&pb.RunIdReq{RunId: runID})
	if err != nil || v.Status != "cancelled" {
		t.Fatalf("run = %+v %v", v, err)
	}
	if tk, _ := env.Svc.Tasks.Get(context.Background(), taskID); tk.Status != task.TaskStatusSolved {
		t.Fatalf("task = %+v", tk)
	}
}
