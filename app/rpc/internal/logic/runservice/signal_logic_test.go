package runservicelogic

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

func repoFile(rel string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", rel)
}

func as(user string) context.Context {
	if user == "" {
		return context.Background()
	}
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(identity.MetadataKey, user))
}

// TestSignalAuthorization 用 VIP 示例的 A1003：首次扫描后升级到 vip_cs_leads 组队列，escalate 节点等待决定。
func TestSignalAuthorization(t *testing.T) {
	platform := testdb.New(t, "deploy/sql/*.sql")
	biz := testdb.New(t, "deploy/mock/appeal.sql")
	testdb.Exec(t, biz, "UPDATE appeal SET updated_at = updated_at - INTERVAL 5 MINUTE")
	clock := engine.NewFakeClock(time.Now())
	s, err := bootstrap.Build(bootstrap.Config{
		DataSource: platform, BizDataSource: biz, Owner: "test",
		RelationsFile: repoFile("deploy/mock/vip_rm.yaml"), GroupsFile: repoFile("deploy/mock/groups.yaml"),
	}, clock)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(repoFile("examples/task_types/vip_appeal_alert.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := spec.FromYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.PublishDirect(context.Background(), s.Conn, body, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	if err := s.Ticker.Tick(context.Background(), struct{ Index, Total int }{0, 1}); err != nil {
		t.Fatal(err)
	}
	var runID int64
	if err := s.Conn.QueryRowCtx(context.Background(), &runID, "select run_id from task where subject_id like 'A1003:%' order by id desc limit 1"); err != nil {
		t.Fatal(err)
	}
	svcCtx := &svc.ServiceContext{Services: s}
	signal := func(user string) (*pb.SignalResp, error) {
		return NewSignalLogic(as(user), svcCtx).Signal(&pb.SignalReq{RunId: runID, NodeId: "escalate", Option: "handled"})
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
