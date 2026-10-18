package runservicelogic

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
)

// TestSignalAuthorization 用 A1003：首次扫描后升级到 vip_cs_leads 组队列，escalate 节点等待决定。
func TestSignalAuthorization(t *testing.T) {
	env := rpctest.New(t)
	_, runID := env.TaskID(t, "A1003")
	signal := func(user string) (*pb.SignalResp, error) {
		return NewSignalLogic(rpctest.As(user), env.Svc).Signal(&pb.SignalReq{RunId: runID, NodeId: "escalate", Option: "handled"})
	}

	if _, err := signal(""); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous: %v", err)
	}
	if res, err := signal("rm.bob@example.com"); err != nil || res.Accepted || res.Code != "forbidden" {
		t.Fatalf("outsider: %+v %v", res, err)
	}
	// 组任务无需先认领：候选组成员直接决定。
	if res, err := signal("lead@example.com"); err != nil || !res.Accepted {
		t.Fatalf("group member: %+v %v", res, err)
	}
	// 同组的另一人再点：任务的处理人已是第一个做决定的人。
	if res, err := signal("lead.carol@example.com"); err != nil || res.Accepted || res.Code != "forbidden" {
		t.Fatalf("second member: %+v %v", res, err)
	}
	if res, err := signal("lead@example.com"); err != nil || res.Accepted || res.Code != string(engine.RejectNotWaiting) {
		t.Fatalf("second click: %+v %v", res, err)
	}
}
