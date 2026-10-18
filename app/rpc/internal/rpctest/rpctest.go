// Package rpctest 为 RPC logic 的集成测试准备环境：临时库、发布 VIP 工单提醒示例、触发一次扫描。
// 需要 WB_TEST_MYSQL，未设置时测试跳过（见 internal/pkg/testdb）。
package rpctest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

// Env 是一套跑过首次扫描的环境。扫描后：A1001、A1002 分配给 rm.alice 并在等定时器；
// A1003 升级到 vip_cs_leads 组队列，escalate 节点等待决定。
type Env struct {
	Svc   *svc.ServiceContext
	Clock *engine.FakeClock
}

// RepoFile 返回仓库内文件的绝对路径。
func RepoFile(rel string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", rel)
}

// New 准备环境。
func New(t *testing.T) *Env {
	t.Helper()
	platform := testdb.New(t, "deploy/sql/*.sql")
	biz := testdb.New(t, "deploy/mock/appeal.sql")
	testdb.Exec(t, biz, "UPDATE appeal SET updated_at = updated_at - INTERVAL 5 MINUTE")
	clock := engine.NewFakeClock(time.Now())
	s, err := bootstrap.Build(bootstrap.Config{
		DataSource: platform, BizDataSource: biz, Owner: "test",
		RelationsFile: RepoFile("deploy/mock/vip_rm.yaml"), GroupsFile: RepoFile("deploy/mock/groups.yaml"),
	}, clock)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(RepoFile("examples/task_types/vip_appeal_alert.yaml"))
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
	return &Env{Svc: &svc.ServiceContext{Services: s}, Clock: clock}
}

// TaskID 返回工单最新任务的 id 与 run id。
func (e *Env) TaskID(t *testing.T, appealID string) (taskID, runID int64) {
	t.Helper()
	var row struct {
		ID    int64 `db:"id"`
		RunID int64 `db:"run_id"`
	}
	if err := e.Svc.Conn.QueryRowPartialCtx(context.Background(), &row, "select id, run_id from task where subject_id like ? order by id desc limit 1", appealID+":%"); err != nil {
		t.Fatal(err)
	}
	return row.ID, row.RunID
}

// As 返回带调用人身份的 context；user 为空时不带身份。
func As(user string) context.Context {
	if user == "" {
		return context.Background()
	}
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(identity.MetadataKey, user))
}
