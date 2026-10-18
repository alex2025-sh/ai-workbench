package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

func repoFile(t *testing.T, rel string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", rel)
}

type harness struct {
	t     *testing.T
	ctx   context.Context
	clock *engine.FakeClock
	svc   *Services
	biz   string
}

func newHarness(t *testing.T) *harness {
	platform := testdb.New(t, "deploy/sql/*.sql")
	biz := testdb.New(t, "deploy/mock/appeal.sql")
	// 扫描只读取 60 秒（lag_seconds）之前更新的行，把样例数据的更新时间往前拨。
	testdb.Exec(t, biz, "UPDATE appeal SET updated_at = updated_at - INTERVAL 5 MINUTE")

	clock := engine.NewFakeClock(time.Now())
	svc, err := Build(Config{DataSource: platform, BizDataSource: biz, RelationsFile: repoFile(t, "deploy/mock/vip_rm.yaml"), Owner: "test"}, clock)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, ctx: context.Background(), clock: clock, svc: svc, biz: biz}
}

func (h *harness) publish(path string) {
	h.t.Helper()
	raw, err := os.ReadFile(repoFile(h.t, path))
	if err != nil {
		h.t.Fatal(err)
	}
	body, err := spec.FromYAML(raw)
	if err != nil {
		h.t.Fatal(err)
	}
	res, err := registry.PublishDirect(h.ctx, h.svc.Conn, body, "test", h.clock.Now())
	if err != nil {
		h.t.Fatal(err)
	}
	if res.Version != 1 {
		h.t.Fatalf("publish: version=%d issues=%+v", res.Version, res.Issues)
	}
}

// tick 把时钟拨到 d 之后，执行一次 wf-ticker：触发到期的来源，执行到期的定时器、超时与重试。
// 引擎没有后台轮询，两次 tick 之间流程只由事件（启动、人工操作、取消）推进。
func (h *harness) tick(d time.Duration) {
	h.t.Helper()
	h.clock.Advance(d)
	if err := h.svc.Ticker.Tick(h.ctx, struct{ Index, Total int }{0, 1}); err != nil {
		h.t.Fatal(err)
	}
}

// tasks 返回工单的全部任务，按创建顺序。去重键是「工单号:提交时间」，重新提交的工单会有新任务。
func (h *harness) tasks(appealID string) []*task.Task {
	h.t.Helper()
	var ids []int64
	if err := h.svc.Conn.QueryRowsCtx(h.ctx, &ids, "select id from task where task_type = 'vip_appeal_alert' and subject_id like ? order by id", appealID+":%"); err != nil {
		h.t.Fatal(err)
	}
	var out []*task.Task
	for _, id := range ids {
		tk, err := h.svc.Tasks.Get(h.ctx, id)
		if err != nil {
			h.t.Fatal(err)
		}
		out = append(out, tk)
	}
	return out
}

// task 返回工单最新的任务。
func (h *harness) task(appealID string) *task.Task {
	h.t.Helper()
	ts := h.tasks(appealID)
	if len(ts) == 0 {
		h.t.Fatalf("no task for %s", appealID)
	}
	return ts[len(ts)-1]
}

func (h *harness) run(appealID string) *engine.RunView {
	h.t.Helper()
	v, err := h.svc.Engine.GetRun(h.ctx, h.task(appealID).RunID)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

func (h *harness) node(appealID, nodeID string) engine.NodeRun {
	for _, n := range h.run(appealID).Nodes {
		if n.NodeID == nodeID {
			return n
		}
	}
	h.t.Fatalf("node %s not found", nodeID)
	return engine.NodeRun{}
}

func (h *harness) cards(appealID string) []string {
	var out []string
	for _, c := range h.svc.Lark.Sent() {
		if strings.Contains(c.Card.Title, appealID) {
			out = append(out, c.Card.Title)
		}
	}
	return out
}

// TestVIPAppealAlert 按 deploy/mock/appeal.sql 的样例数据跑通 examples/task_types/vip_appeal_alert.yaml：
// 扫描 → 建任务 → 通知 RM → 48 小时提醒 → 7 天升级 → 组长处理，以及工单中途审核完成时自动关闭；
// 时间用可快进的时钟推进。
func TestVIPAppealAlert(t *testing.T) {
	h := newHarness(t)
	h.publish("examples/task_types/vip_appeal_alert.yaml")

	// 第一次触发：cron 是每分钟一次，拨到下一分钟。
	h.tick(time.Minute)

	// 只有 VIP 且待审核的 A1001 / A1002 / A1003 建任务；A1004 已通过，A1005 非 VIP，A1006 未提交。
	for _, id := range []string{"A1004", "A1005", "A1006"} {
		if ts := h.tasks(id); len(ts) != 0 {
			t.Errorf("%s should be filtered", id)
		}
	}

	// A1001 刚提交：通知了 RM，等 48 小时。
	if c := h.cards("A1001"); len(c) != 1 || !strings.Contains(c[0], "待审核") {
		t.Fatalf("A1001 cards = %v", c)
	}
	if tk := h.task("A1001"); tk.Status != task.TaskStatusPending || tk.AssigneeID != "rm.alice@example.com" || tk.CurrentNode != "notify_rm" {
		t.Fatalf("A1001 task = %+v", tk)
	}
	if n := h.node("A1001", "wait_48h"); n.Status != engine.NodeWaiting {
		t.Fatalf("A1001 wait_48h = %s", n.Status)
	}

	// A1002 已提交 50 小时：通知后立即触发 48 小时提醒，然后等到 7 天。
	if c := h.cards("A1002"); len(c) != 2 || !strings.Contains(c[1], "48 小时") {
		t.Fatalf("A1002 cards = %v", c)
	}
	if n := h.node("A1002", "wait_7d"); n.Status != engine.NodeWaiting {
		t.Fatalf("A1002 wait_7d = %s", n.Status)
	}

	// A1003 已提交 8 天：通知、提醒后升级给 VIP 客服组长，进入组队列待认领。
	if c := h.cards("A1003"); len(c) != 3 || !strings.Contains(c[2], "超过 7 天") {
		t.Fatalf("A1003 cards = %v", c)
	}
	tk := h.task("A1003")
	if tk.Status != task.TaskStatusUnassigned || tk.AssigneeGroup != "vip_cs_leads" || tk.CurrentNode != "escalate" {
		t.Fatalf("A1003 task = %+v", tk)
	}
	esc := h.node("A1003", "escalate")
	if esc.Status != engine.NodeWaiting {
		t.Fatalf("A1003 escalate = %s", esc.Status)
	}

	// 卡片上的 uid 已按 x-pii 脱敏。
	for _, c := range h.svc.Lark.Sent() {
		for _, f := range c.Card.Fields {
			if f.Label == "用户 UID" && !strings.Contains(f.Value, "*") {
				t.Fatalf("uid not masked: %q", f.Value)
			}
		}
	}

	// 再触发一次：数据没有变化，不会重复建任务或重复发卡片。
	before := len(h.svc.Lark.Sent())
	h.tick(time.Minute)
	if after := len(h.svc.Lark.Sent()); after != before {
		t.Fatalf("duplicate cards: %d → %d", before, after)
	}

	// 组长处理 A1003：推迟暂不支持，未知选项被拒绝，处理后流程结束、任务完结。
	runID := h.task("A1003").RunID
	for _, c := range []struct{ option, reason string }{{"snooze", "推迟"}, {"nope", "未知选项"}} {
		res, err := h.svc.Engine.Signal(h.ctx, engine.SignalCmd{RunID: runID, NodeID: "escalate", Option: c.option, Actor: "lead@example.com"})
		if err != nil || res.Accepted || !strings.Contains(res.Reason, c.reason) {
			t.Fatalf("signal %s: %+v %v", c.option, res, err)
		}
	}
	res, err := h.svc.Engine.Signal(h.ctx, engine.SignalCmd{RunID: runID, NodeID: "escalate", Option: "handled", Actor: "lead@example.com"})
	if err != nil || !res.Accepted {
		t.Fatalf("signal handled: %+v %v", res, err)
	}
	res, _ = h.svc.Engine.Signal(h.ctx, engine.SignalCmd{RunID: runID, NodeID: "escalate", Option: "handled", Actor: "lead@example.com"})
	if res.Accepted {
		t.Fatal("second click must be rejected")
	}
	// 人工操作是事件：Signal 返回时流程已经推进到结束，不需要等 tick。
	if v := h.run("A1003"); v.Run.Status != engine.RunSucceeded {
		t.Fatalf("A1003 run = %s", v.Run.Status)
	}
	// 做决定的组长成为任务的处理人：组任务没人认领，也能记下是谁处理的。
	if tk := h.task("A1003"); tk.Status != task.TaskStatusSolved || tk.Resolution != "escalated" || tk.AssigneeID != "lead@example.com" || tk.AssigneeGroup != "" {
		t.Fatalf("A1003 task = %+v", tk)
	}

	// 48 小时后：A1001 仍待审核，提醒 RM。
	h.tick(48 * time.Hour)
	if c := h.cards("A1001"); len(c) != 2 {
		t.Fatalf("A1001 cards after 48h = %v", c)
	}

	// A1001 在等待 7 天期间审核通过：下一次扫描命中 close_when，取消 run，任务以 auto_closed 完结，
	// 7 天定时器随之撤销，不再检查或升级。
	testdb.Exec(t, h.biz, "UPDATE appeal SET appeal_status = 'PASS', updated_at = NOW() - INTERVAL 3 MINUTE WHERE appeal_id = 'A1001'")
	h.tick(time.Minute)
	if v := h.run("A1001"); v.Run.Status != engine.RunCancelled {
		t.Fatalf("A1001 run = %s", v.Run.Status)
	}
	// 自动关闭不改处理人：任务仍记在原 RM 名下。
	if tk := h.task("A1001"); tk.Status != task.TaskStatusSolved || tk.Resolution != "auto_closed" || tk.AssigneeID != "rm.alice@example.com" {
		t.Fatalf("A1001 task = %+v", tk)
	}
	var pending int
	if err := h.svc.Conn.QueryRowCtx(h.ctx, &pending, "select count(*) from wf_job where run_id = ? and status = 'pending'", h.task("A1001").RunID); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("A1001 still has %d pending jobs", pending)
	}

	h.tick(6 * 24 * time.Hour)
	if c := h.cards("A1001"); len(c) != 2 {
		t.Fatalf("A1001 must not escalate: %v", c)
	}

	// A1002 到 7 天时仍待审核：升级给组长。
	if c := h.cards("A1002"); len(c) != 3 {
		t.Fatalf("A1002 cards after 7d = %v", c)
	}
	if n := h.node("A1002", "escalate"); n.Status != engine.NodeWaiting {
		t.Fatalf("A1002 escalate = %s", n.Status)
	}

	// A1001 被驳回后重新提交：提交时间变了，是一次新的审核，建新任务并重新通知 RM；旧任务保持自动关闭。
	testdb.Exec(t, h.biz, "UPDATE appeal SET appeal_status = 'COMMITED', commit_time = ?, updated_at = NOW() - INTERVAL 2 MINUTE WHERE appeal_id = 'A1001'", h.clock.Now().Unix())
	h.tick(time.Minute)
	ts := h.tasks("A1001")
	if len(ts) != 2 || ts[0].Resolution != "auto_closed" || ts[1].Status != task.TaskStatusPending || ts[1].AssigneeID != "rm.alice@example.com" {
		t.Fatalf("A1001 tasks after resubmission = %d", len(ts))
	}
	if c := h.cards("A1001"); len(c) != 3 || !strings.Contains(c[2], "待审核") {
		t.Fatalf("A1001 cards after resubmission = %v", c)
	}
	if n := h.node("A1001", "wait_48h"); n.Status != engine.NodeWaiting {
		t.Fatalf("resubmitted A1001 wait_48h = %s", n.Status)
	}
}
