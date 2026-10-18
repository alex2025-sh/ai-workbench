package runservicelogic

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
)

func TestRunQueries(t *testing.T) {
	env := rpctest.New(t)
	ops := rpctest.As("ops@example.com")
	list := func(req *pb.ListRunsReq) *pb.ListRunsResp {
		t.Helper()
		res, err := NewListRunsLogic(ops, env.Svc).ListRuns(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// 首次扫描为 A1001 / A1002 / A1003 各启动一个 run，都还在运行。
	all := list(&pb.ListRunsReq{})
	if len(all.Items) != 3 || all.NextCursor != "" {
		t.Fatalf("all = %+v", all)
	}
	first := list(&pb.ListRunsReq{Page: &pb.PageReq{PageSize: 2}})
	second := list(&pb.ListRunsReq{Page: &pb.PageReq{PageSize: 2, Cursor: first.NextCursor}})
	if len(first.Items) != 2 || first.NextCursor == "" || len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("pages = %d+%d next=%q", len(first.Items), len(second.Items), first.NextCursor)
	}
	seen := map[int64]bool{}
	for _, r := range append(first.Items, second.Items...) {
		if seen[r.RunId] {
			t.Fatalf("run %d listed twice", r.RunId)
		}
		seen[r.RunId] = true
	}

	taskID, runID := env.TaskID(t, "A1003")
	if got := list(&pb.ListRunsReq{TaskId: taskID}); len(got.Items) != 1 || got.Items[0].RunId != runID {
		t.Fatalf("by task = %+v", got)
	}
	if got := list(&pb.ListRunsReq{TaskId: 999999}); len(got.Items) != 0 {
		t.Fatalf("unknown task = %+v", got)
	}
	if got := list(&pb.ListRunsReq{TaskTypeKey: "vip_appeal_alert", Status: "running"}); len(got.Items) != 3 {
		t.Fatalf("by type and status = %d", len(got.Items))
	}
	if got := list(&pb.ListRunsReq{Status: "needs_intervention"}); len(got.Items) != 0 {
		t.Fatalf("needs_intervention = %d", len(got.Items))
	}

	v, err := NewGetRunLogic(ops, env.Svc).GetRun(&pb.RunIdReq{RunId: runID})
	if err != nil {
		t.Fatal(err)
	}
	var graph struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(v.GraphJson, &graph); err != nil || len(graph.Nodes) != len(v.Nodes) {
		t.Fatalf("graph has %d nodes, view has %d (%v)", len(graph.Nodes), len(v.Nodes), err)
	}
	if v.Nodes[0].NodeId != "start" || v.Status != "running" {
		t.Fatalf("view = %+v", v)
	}
	if _, err := NewGetRunLogic(ops, env.Svc).GetRun(&pb.RunIdReq{RunId: 999999}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing run: %v", err)
	}

	evs, err := NewGetRunEventsLogic(ops, env.Svc).GetRunEvents(&pb.GetRunEventsReq{RunId: runID, Page: &pb.PageReq{PageSize: 3}})
	if err != nil || len(evs.Items) != 3 || evs.NextCursor == "" {
		t.Fatalf("events page = %+v %v", evs, err)
	}
	rest, err := NewGetRunEventsLogic(ops, env.Svc).GetRunEvents(&pb.GetRunEventsReq{RunId: runID, Page: &pb.PageReq{PageSize: 100, Cursor: evs.NextCursor}})
	if err != nil || len(rest.Items) == 0 || rest.Items[0].Id == evs.Items[2].Id {
		t.Fatalf("events rest = %+v %v", rest, err)
	}
}
