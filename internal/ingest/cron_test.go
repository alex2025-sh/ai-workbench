package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/ingest"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

const scanType = `
key: scan_test
name: scan test
schema:
  type: object
  required: [id, status]
  properties: { id: { type: string }, status: { type: string }, n: { type: integer } }
sources:
  - id: src
    kind: cron
    schedule: "*/1 * * * *"
    skill: test.scan
    filter: record.status == "OPEN"
    close_when: record.status == "DONE"
    mapping: { id: record.id, status: record.status, n: record.n }
dedup_key: "{{ payload.id }}"
views: { card: { title: "t {{ payload.id }}", fields: [ { value: "{{ payload.status }}" } ] } }
view: card
workflow:
  nodes: [ { id: start, type: trigger }, { id: done, type: end } ]
  edges: [ { id: e1, from: start, port: next, to: done } ]
`

// flakyTasks 在为指定记录建任务时失败，模拟数据库抖动。
type flakyTasks struct {
	*task.Store
	failOn atomic.Value // string：SubjectID
}

func (f *flakyTasks) Create(ctx context.Context, cmd task.CreateCmd) (*task.Task, bool, error) {
	if v, _ := f.failOn.Load().(string); v != "" && v == cmd.SubjectID {
		return nil, false, errors.New("db unavailable")
	}
	return f.Store.Create(ctx, cmd)
}

type fakeRuns struct {
	next      atomic.Int64
	started   atomic.Int32
	cancelled atomic.Int32
}

func (r *fakeRuns) StartRun(context.Context, engine.StartRunCmd) (engine.Run, bool, error) {
	r.started.Add(1)
	return engine.Run{ID: r.next.Add(1)}, true, nil
}

func (r *fakeRuns) Cancel(context.Context, int64, string, string) error {
	r.cancelled.Add(1)
	return nil
}

type fixture struct {
	t      *testing.T
	ctx    context.Context
	conn   sqlx.SqlConn
	clock  *engine.FakeClock
	tasks  *flakyTasks
	runs   *fakeRuns
	runner *ingest.CronRunner
	items  []map[string]any
}

func newFixture(t *testing.T) *fixture {
	conn := sqlx.NewMysql(testdb.New(t, "deploy/sql/*.sql"))
	f := &fixture{t: t, ctx: context.Background(), conn: conn, clock: engine.NewFakeClock(time.Date(2026, 10, 1, 9, 0, 30, 0, time.UTC))}
	body, err := spec.FromYAML([]byte(scanType))
	if err != nil {
		t.Fatal(err)
	}
	if res, err := registry.PublishDirect(f.ctx, conn, body, "test", f.clock.Now()); err != nil || res.Version != 1 {
		t.Fatalf("publish: %v %+v", err, res)
	}
	// 扫描 skill：游标为空时返回全部记录，否则返回空。
	skills := &action.Skills{Builtin: map[string]action.SkillFunc{"test.scan": func(_ context.Context, in json.RawMessage) (any, error) {
		var req struct{ Cursor string }
		_ = json.Unmarshal(in, &req)
		if req.Cursor != "" {
			return map[string]any{"items": []any{}, "next_cursor": req.Cursor}, nil
		}
		return map[string]any{"items": f.items, "next_cursor": "end"}, nil
	}}}
	rt := action.NewRuntime()
	if err := rt.Register(skills); err != nil {
		t.Fatal(err)
	}
	f.tasks = &flakyTasks{Store: task.NewStore(conn)}
	f.tasks.failOn.Store("")
	f.runs = &fakeRuns{}
	f.runner = ingest.NewCronRunner(conn, engine.NewPlans(conn), f.tasks, f.runs, rt, f.clock)
	return f
}

func (f *fixture) fire() ingest.FireResult {
	f.t.Helper()
	f.clock.Advance(time.Minute)
	res := f.runner.FireDue(f.ctx)
	if len(res) != 1 {
		f.t.Fatalf("fired %d schedules", len(res))
	}
	return res[0]
}

func (f *fixture) cursor() string {
	f.t.Helper()
	var c string
	if err := f.conn.QueryRowCtx(f.ctx, &c, "select scan_cursor from wf_schedule where name = ?", registry.ScheduleName("scan_test", "src")); err != nil {
		f.t.Fatal(err)
	}
	return c
}

// TestTransientFailureKeepsCursor：建任务时数据库抖动，本次扫描停下且不推进游标；下次从同一位置重扫，记录不丢也不重复。
func TestTransientFailureKeepsCursor(t *testing.T) {
	f := newFixture(t)
	f.items = []map[string]any{{"id": "R1", "status": "OPEN"}, {"id": "R2", "status": "OPEN"}, {"id": "R3", "status": "OPEN"}}
	f.tasks.failOn.Store("R2")
	res := f.fire()
	if res.Err == nil || res.Created != 1 || f.cursor() != "" {
		t.Fatalf("transient failure must stop the scan and keep the cursor: %+v cursor=%q", res, f.cursor())
	}
	f.tasks.failOn.Store("")
	res = f.fire()
	if res.Err != nil || res.Created != 2 || f.cursor() != "end" {
		t.Fatalf("rescan: %+v cursor=%q", res, f.cursor())
	}
	if n := f.runs.started.Load(); n != 3 {
		t.Fatalf("runs started = %d, want one per record", n)
	}
}

// TestBadRecordSkipped：坏数据（不符合 schema）跳过并计数，不阻塞游标。
func TestBadRecordSkipped(t *testing.T) {
	f := newFixture(t)
	f.items = []map[string]any{{"id": "R1", "status": "OPEN", "n": "not a number"}, {"id": "R2", "status": "OPEN"}}
	res := f.fire()
	if res.Err != nil || res.Invalid != 1 || res.Created != 1 || f.cursor() != "end" {
		t.Fatalf("scan: %+v cursor=%q", res, f.cursor())
	}
}

// TestCloseUnstartedTask：任务建了但 run 没启动，之后源记录满足 close_when：直接关闭任务，而不是让它一直待分配。
func TestCloseUnstartedTask(t *testing.T) {
	f := newFixture(t)
	tk, _, err := f.tasks.Store.Create(f.ctx, task.CreateCmd{TaskType: "scan_test", TypeVersion: 1, SubjectType: "scan_test", SubjectID: "R9",
		DedupKey: "scan_test:R9", Payload: json.RawMessage(`{"id":"R9","status":"OPEN"}`)})
	if err != nil {
		t.Fatal(err)
	}
	f.items = []map[string]any{{"id": "R9", "status": "DONE"}}
	res := f.fire()
	if res.Err != nil || res.Closed != 1 || f.runs.cancelled.Load() != 0 {
		t.Fatalf("scan: %+v cancelled=%d", res, f.runs.cancelled.Load())
	}
	got, err := f.tasks.Get(f.ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.TaskStatusSolved || got.Resolution != task.ResolutionAutoClosed {
		t.Fatalf("task = %+v", got)
	}
}
