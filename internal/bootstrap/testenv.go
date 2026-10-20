package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// TestEnv 是端到端测试的环境：临时平台库（含 mock 团队）、mock 业务库、三个 VIP 示例已发布、可拨动的时钟。
type TestEnv struct {
	*Services
	Platform string // 平台库 DSN
	Biz      string // 业务库 DSN
	clock    struct {
		sync.Mutex
		now time.Time
	}
}

// Now 返回测试时钟的当前时间。
func (e *TestEnv) Now() time.Time {
	e.clock.Lock()
	defer e.clock.Unlock()
	return e.clock.now
}

// Advance 拨动测试时钟。
func (e *TestEnv) Advance(d time.Duration) {
	e.clock.Lock()
	defer e.clock.Unlock()
	e.clock.now = e.clock.now.Add(d)
}

// RepoFile 返回仓库内文件的绝对路径。
func RepoFile(rel string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", rel)
}

// NewTestEnv 准备环境并发布 examples/task_types 下的全部示例。需要 WB_TEST_MYSQL，未设置时跳过。
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()
	e := &TestEnv{Platform: testdb.New(t, "deploy/sql/*.sql", "deploy/mock/teams.sql"), Biz: testdb.New(t, "deploy/mock/appeal.sql")}
	e.clock.now = time.Now().Truncate(time.Second)
	s, err := Build(Config{
		DataSource: e.Platform, Datasources: map[string]string{"appeal_ro": e.Biz},
		RelationsFile: RepoFile("deploy/mock/vip_rm.yaml"), PermissionsFile: RepoFile("deploy/mock/permissions.yaml"),
		WebBase: "http://wb.local",
	}, e.Now)
	if err != nil {
		t.Fatal(err)
	}
	s.Notify.Inline = true
	e.Services = s
	files, _ := filepath.Glob(RepoFile("examples/task_types/*.yaml"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		body, err := tasktype.FromYAML(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, issues, err := s.Registry.PublishBody(context.Background(), body, "test", e.Now()); err != nil {
			t.Fatalf("publish %s: %v %v", f, err, issues)
		}
	}
	return e
}
