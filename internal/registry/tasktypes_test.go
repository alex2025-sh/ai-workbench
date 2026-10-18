package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

func TestSkeletonIsValid(t *testing.T) {
	res := spec.Check(registry.Skeleton("demo_new", "新任务类型", "demo", "", "dev@example.com"))
	if !res.OK() {
		t.Fatalf("skeleton issues: %+v", res.Issues)
	}
}

func TestContentHashIgnoresUI(t *testing.T) {
	a := registry.ContentHash(json.RawMessage(`{"key":"k","ui":{"canvas":{"x":1}},"name":"n"}`))
	b := registry.ContentHash(json.RawMessage(`{"name":"n","key":"k","ui":{"canvas":{"x":2}}}`))
	c := registry.ContentHash(json.RawMessage(`{"name":"m","key":"k"}`))
	if a == "" || a != b || a == c {
		t.Fatalf("hashes: %s %s %s", a, b, c)
	}
}

func repoFile(rel string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", rel)
}

func TestTaskTypeDrafts(t *testing.T) {
	ctx := context.Background()
	conn := sqlx.NewMysql(testdb.New(t, "deploy/sql/*.sql"))
	r := registry.NewTaskTypes(conn)

	// wbctl 发布过、没有登记过的任务类型：打开时补登记，并基于最新版本创建草稿。
	raw, err := os.ReadFile(repoFile("examples/task_types/vip_appeal_alert.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	vip, _ := spec.FromYAML(raw)
	if _, err := registry.PublishDirect(ctx, conn, vip, "wbctl", time.Now()); err != nil {
		t.Fatal(err)
	}
	d, err := r.Get(ctx, "vip_appeal_alert", "dev@example.com")
	if err != nil || d.BaseVersion != 1 || d.Revision != 0 {
		t.Fatalf("draft = %+v %v", d, err)
	}
	if _, err := r.Get(ctx, "nope", "dev@example.com"); !errors.Is(err, registry.ErrNoTaskType) {
		t.Fatalf("missing type: %v", err)
	}

	// 新建：key 不能和已发布的重复。
	if _, err := r.Create(ctx, "vip_appeal_alert", "x", "demo", "", "dev@example.com"); !errors.Is(err, registry.ErrExists) {
		t.Fatalf("duplicate key: %v", err)
	}
	nd, err := r.Create(ctx, "demo_new", "新任务类型", "demo", "", "dev@example.com")
	if err != nil || nd.BaseVersion != 0 {
		t.Fatalf("create: %+v %v", nd, err)
	}
	if _, err := r.Create(ctx, "demo_new", "again", "demo", "", "dev@example.com"); !errors.Is(err, registry.ErrExists) {
		t.Fatalf("duplicate create: %v", err)
	}

	// 保存：乐观锁、key 不可改；只移动坐标时内容哈希不变。
	var body map[string]any
	_ = json.Unmarshal(nd.Body, &body)
	body["ui"] = map[string]any{"canvas": map[string]any{"positions": map[string]any{"start": map[string]int{"x": 10, "y": 20}}}}
	moved, _ := json.Marshal(body)
	saved, issues, err := r.Save(ctx, "demo_new", nd.Revision, moved, "dev@example.com")
	if err != nil || saved.Revision != nd.Revision+1 || len(issues) != 0 || saved.ContentHash != nd.ContentHash {
		t.Fatalf("save: %+v %v %v", saved, issues, err)
	}
	if _, _, err := r.Save(ctx, "demo_new", nd.Revision, moved, "dev@example.com"); !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	body["key"] = "renamed"
	renamed, _ := json.Marshal(body)
	if _, _, err := r.Save(ctx, "demo_new", saved.Revision, renamed, "dev@example.com"); !errors.Is(err, registry.ErrKeyChanged) {
		t.Fatalf("key change: %v", err)
	}

	// 有错误的草稿也能保存（返回问题），但不能发布。
	body["key"] = "demo_new"
	body["workflow"] = map[string]any{"nodes": []any{map[string]any{"id": "start", "type": "trigger"}}, "edges": []any{}}
	broken, _ := json.Marshal(body)
	saved, issues, err = r.Save(ctx, "demo_new", saved.Revision, broken, "dev@example.com")
	if err != nil || len(issues) == 0 {
		t.Fatalf("broken save: %v %v", issues, err)
	}
	if _, _, err := r.PublishDraft(ctx, "demo_new", saved.Revision, "dev@example.com"); !errors.Is(err, registry.ErrHasErrors) {
		t.Fatalf("publish broken: %v", err)
	}

	list, err := r.List(ctx)
	if err != nil || len(list) != 2 || list[0].Key != "demo_new" || list[0].LatestVersion != 0 || !list[0].HasDraft || list[1].LatestVersion != 1 || !list[1].HasDraft {
		t.Fatalf("list = %+v %v", list, err)
	}

	// 丢弃后重新新建不行（配置对象还在），恢复正确内容后发布。
	if err := r.Discard(ctx, "vip_appeal_alert", 99); !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("discard stale: %v", err)
	}
	if err := r.Discard(ctx, "vip_appeal_alert", d.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Find(ctx, "vip_appeal_alert"); !errors.Is(err, registry.ErrNoDraft) {
		t.Fatalf("after discard: %v", err)
	}
	saved, _, err = r.Save(ctx, "demo_new", saved.Revision, moved, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := r.PublishDraft(ctx, "demo_new", saved.Revision, "dev@example.com")
	if err != nil || v != 1 {
		t.Fatalf("publish: %d %v", v, err)
	}
	if _, err := r.Find(ctx, "demo_new"); !errors.Is(err, registry.ErrNoDraft) {
		t.Fatalf("draft should be gone after publish: %v", err)
	}
	if d, err := r.Get(ctx, "demo_new", "dev@example.com"); err != nil || d.BaseVersion != 1 {
		t.Fatalf("reopen after publish: %+v %v", d, err)
	}
}
