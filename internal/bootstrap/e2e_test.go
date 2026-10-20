package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/notify"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/source"
	"github.com/alex2025-sh/ai-workbench/internal/task"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

func tick(t *testing.T, e *TestEnv) {
	t.Helper()
	e.Advance(time.Minute)
	if err := e.Ticker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// open 返回某个任务类型中 appeal 的跟踪中任务。
func open(t *testing.T, e *TestEnv, taskType, appeal string) *task.Task {
	t.Helper()
	tracked, err := e.Tasks.ListTracked(context.Background(), taskType)
	if err != nil {
		t.Fatal(err)
	}
	for i := range tracked {
		if v := tasktype.DecodeValues(tracked[i].Fields)["appeal_id"]; fmt.Sprint(v) == appeal {
			return &tracked[i]
		}
	}
	return nil
}

func sent(e *TestEnv, target string) []notify.Sent {
	var out []notify.Sent
	for _, s := range e.Lark.Sent() {
		if s.Target == target {
			out = append(out, s)
		}
	}
	return out
}

func TestVIPAppeal(t *testing.T) {
	e := NewTestEnv(t)
	ctx := context.Background()
	tick(t, e)

	// 首次扫描：每张待审核的工单按提交时间落进一个任务类型。
	a1 := open(t, e, "vip_appeal_pending", "A1001")
	a7 := open(t, e, "vip_appeal_pending", "A1007")
	a2 := open(t, e, "vip_appeal_48h", "A1002")
	a3 := open(t, e, "vip_appeal_7d", "A1003")
	switch {
	case a1 == nil || a1.AssigneeID != "rm.alice@example.com" || a1.Status != task.TaskStatusPending || a1.Priority != task.TaskPriorityP2:
		t.Fatalf("A1001 = %+v", a1)
	case a7 == nil || !a7.InQueue() || a7.TeamKey != "vip_cs_leads" || a7.TeamMode != "claim":
		t.Fatalf("A1007 (no RM) = %+v", a7)
	case a2 == nil || a2.AssigneeID != "rm.alice@example.com" || a2.NodeID != "rm" || a2.Priority != task.TaskPriorityP1:
		t.Fatalf("A1002 = %+v", a2)
	case a3 == nil || !a3.InQueue() || a3.TeamKey != "vip_review" || a3.TeamMode != "assign":
		t.Fatalf("A1003 = %+v", a3)
	}
	for _, other := range []string{"A1004", "A1005", "A1006"} {
		for _, tt := range []string{"vip_appeal_pending", "vip_appeal_48h", "vip_appeal_7d"} {
			if open(t, e, tt, other) != nil {
				t.Errorf("%s should not have a task in %s", other, tt)
			}
		}
	}
	// 通知：RM 私聊两条；认领模式发到组长群；分配模式私发给分配者。
	if got := sent(e, "rm.alice@example.com"); len(got) != 2 {
		t.Fatalf("alice got %d cards", len(got))
	}
	if got := sent(e, "oc_vip_cs_leads"); len(got) != 1 || got[0].Kind != notify.KindChat {
		t.Fatalf("leads chat = %+v", got)
	}
	card := sent(e, "lead@example.com")
	if len(card) != 1 || card[0].Card.NodeName != "VIP 审核小组" || card[0].Card.TaskURL != fmt.Sprintf("http://wb.local/tasks/%d", a3.ID) {
		t.Fatalf("assigner card = %+v", card)
	}
	if f := card[0].Card.Fields; len(f) == 0 || f[0].Label != "工单类型" {
		t.Fatalf("card fields = %+v", f)
	}

	// 再扫一次：没有变化，不重复建任务、不重发通知。
	before := len(e.Lark.Sent())
	tick(t, e)
	if got := len(e.Lark.Sent()); got != before {
		t.Fatalf("second scan sent %d new cards", got-before)
	}

	// A1002：RM 升级给组长 → 进入 vip_cs_leads 认领队列，通知发到组长群。
	if _, err := e.Tasks.Do(ctx, a2.ID, task.Event{Kind: task.EvAct, Actor: "rm.alice@example.com", Action: "escalate", ExpectNode: "rm",
		Target: targetOf(t, e, "vip_appeal_48h", "lead")}); err != nil {
		t.Fatal(err)
	}
	if got := sent(e, "oc_vip_cs_leads"); len(got) != 2 || got[1].Card.NodeName != "VIP 客服组长" {
		t.Fatalf("escalation card = %+v", got)
	}
	// 组员认领；原 RM 仍能看到（经手人），但不能再处理。
	if _, err := e.Tasks.Do(ctx, a2.ID, task.Event{Kind: task.EvClaim, Actor: "lead.carol@example.com"}); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.Tasks.Get(ctx, a2.ID)
	if acc, _ := e.Tasks.AccessOf(ctx, cur, "rm.alice@example.com", false); !acc.View {
		t.Fatal("previous handler should still see the task")
	}
	if acc, _ := e.Tasks.AccessOf(ctx, cur, "stranger@example.com", false); acc.View {
		t.Fatal("stranger should not see the task")
	}
	if _, err := e.Tasks.Do(ctx, a2.ID, task.Event{Kind: task.EvAct, Actor: "rm.alice@example.com", Action: "close"}); !errors.Is(err, task.ErrForbidden) {
		t.Fatalf("previous handler close: %v", err)
	}
	if _, err := e.Tasks.Do(ctx, a2.ID, task.Event{Kind: task.EvAct, Actor: "lead.carol@example.com", Action: "close"}); err != nil {
		t.Fatal(err)
	}
	// 手工完结后工单仍在结果里：不重建。
	tick(t, e)
	if got := open(t, e, "vip_appeal_48h", "A1002"); got == nil || got.ID != a2.ID || got.Status != task.TaskStatusSolved {
		t.Fatalf("closed A1002 should stay tracked: %+v", got)
	}
	mine, _ := e.Tasks.List(ctx, task.ListQuery{Scope: task.ScopeInvolved, User: "rm.alice@example.com", Limit: 20})
	if len(mine) != 1 || mine[0].ID != a2.ID {
		t.Fatalf("alice involved = %+v", mine)
	}

	// A1003：组长分配给组员；组员推迟 1 小时，到期回到待处理并重新通知。
	queue, _ := e.Tasks.List(ctx, task.ListQuery{Scope: task.ScopeQueue, User: "lead@example.com", Limit: 20})
	if len(queue) != 2 { // A1007（组长是 vip_cs_leads 成员）与 A1003（vip_review 分配者）
		t.Fatalf("lead queue = %d", len(queue))
	}
	if q, _ := e.Tasks.List(ctx, task.ListQuery{Scope: task.ScopeQueue, User: "reviewer.dave@example.com", Limit: 20}); len(q) != 0 {
		t.Fatalf("executor should not see assign-mode queue: %+v", q)
	}
	if _, err := e.Tasks.Do(ctx, a3.ID, task.Event{Kind: task.EvAssign, Actor: "reviewer.dave@example.com", Assignee: "reviewer.dave@example.com"}); !errors.Is(err, task.ErrForbidden) {
		t.Fatalf("executor assign: %v", err)
	}
	if _, err := e.Tasks.Do(ctx, a3.ID, task.Event{Kind: task.EvAssign, Actor: "lead@example.com", Assignee: "outsider@example.com"}); !errors.Is(err, task.ErrState) {
		t.Fatalf("assign outsider: %v", err)
	}
	if _, err := e.Tasks.Do(ctx, a3.ID, task.Event{Kind: task.EvAssign, Actor: "lead@example.com", Assignee: "reviewer.dave@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := sent(e, "reviewer.dave@example.com"); len(got) != 1 {
		t.Fatalf("dave cards = %d", len(got))
	}
	if _, err := e.Tasks.Do(ctx, a3.ID, task.Event{Kind: task.EvAct, Actor: "reviewer.dave@example.com", Action: "snooze", Snooze: time.Hour}); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if cur, _ := e.Tasks.Get(ctx, a3.ID); cur.Status != task.TaskStatusSnoozed {
		t.Fatalf("woke too early: %+v", cur)
	}
	e.Advance(time.Hour)
	tick(t, e)
	if cur, _ := e.Tasks.Get(ctx, a3.ID); cur.Status != task.TaskStatusPending || cur.AssigneeID != "reviewer.dave@example.com" {
		t.Fatalf("after wake: %+v", cur)
	}
	if got := sent(e, "reviewer.dave@example.com"); len(got) != 2 {
		t.Fatalf("dave should be notified again on wake, got %d", len(got))
	}

	// A1001 审核通过：下一轮扫描自动关闭。
	testdb.Exec(t, e.Biz, "UPDATE appeal SET appeal_status = 'PASS' WHERE appeal_id = 'A1001'")
	tick(t, e)
	if cur, _ := e.Tasks.Get(ctx, a1.ID); cur.Status != task.TaskStatusSolved || cur.Resolution != task.ResolutionAutoClosed || cur.Tracked {
		t.Fatalf("A1001 = %+v", cur)
	}

	// A1007 跨过 48 小时窗口：离开「待审核」（自动关闭），进入「已等待 48 小时」成为新任务。
	testdb.Exec(t, e.Biz, "UPDATE appeal SET commit_time = commit_time - 49 * 3600 WHERE appeal_id = 'A1007'")
	tick(t, e)
	if cur, _ := e.Tasks.Get(ctx, a7.ID); cur.Resolution != task.ResolutionAutoClosed {
		t.Fatalf("A1007 old window = %+v", cur)
	}
	if got := open(t, e, "vip_appeal_48h", "A1007"); got == nil || got.TeamKey != "vip_cs_leads" {
		t.Fatalf("A1007 new window = %+v", got)
	}

	// A1002 审核完成后被驳回重新提交：先离开结果（解除跟踪），再以新的提交时间出现，生成新任务。
	testdb.Exec(t, e.Biz, "UPDATE appeal SET appeal_status = 'REJECTED' WHERE appeal_id = 'A1002'")
	tick(t, e)
	testdb.Exec(t, e.Biz, "UPDATE appeal SET appeal_status = 'COMMITED', commit_time = commit_time + 60 WHERE appeal_id = 'A1002'")
	tick(t, e)
	if got := open(t, e, "vip_appeal_48h", "A1002"); got == nil || got.ID == a2.ID || got.Status != task.TaskStatusPending {
		t.Fatalf("resubmitted A1002 = %+v", got)
	}

	// 任务历史。
	hist, _ := e.Tasks.History(ctx, a2.ID)
	var kinds []string
	for _, h := range hist {
		kinds = append(kinds, h.Kind)
	}
	if fmt.Sprint(kinds) != "[created escalate claimed close gone]" {
		t.Fatalf("A1002 history = %v", kinds)
	}
}

// targetOf 求某个节点的去向（escalate 前由调用方在事务外求好）。
func targetOf(t *testing.T, e *TestEnv, key, node string) (to struct{ Assignee, Team, Mode string }) {
	t.Helper()
	def, _, err := e.Registry.Latest(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	n, _, _ := def.Node(node)
	to.Team, to.Mode = n.Assignee.Team, n.Assignee.Mode
	return to
}

func TestOutboxRetry(t *testing.T) {
	e := NewTestEnv(t)
	fail := true
	e.Lark.Fail = func(kind, target string) error {
		if fail {
			return errors.New("lark down")
		}
		return nil
	}
	retryTick(e)
	if n := len(e.Lark.Sent()); n != 0 {
		t.Fatalf("sent %d while lark is down", n)
	}
	// 第一次失败后 1 分钟重试；仍失败则退避翻倍（2 分钟）。
	retryTick(e)
	if n := len(e.Lark.Sent()); n != 0 {
		t.Fatalf("sent %d while lark is down", n)
	}
	fail = false
	tick(t, e) // 距上次失败 1 分钟，第二次的 2 分钟退避未到
	if n := len(e.Lark.Sent()); n != 0 {
		t.Fatalf("retried before backoff: %d", n)
	}
	retryTick(e)
	if n := len(e.Lark.Sent()); n != 4 {
		t.Fatalf("after retry sent %d, want 4", n)
	}
}

// retryTick 拨动时钟并触发一次 tick，忽略投递失败（Tick 会把失败报出来）。
func retryTick(e *TestEnv) {
	e.Advance(time.Minute)
	_ = e.Ticker.Tick(context.Background())
}

func TestPush(t *testing.T) {
	e := NewTestEnv(t)
	ctx := context.Background()
	body := []byte(`{"key":"kyc_review","name":"KYC 复核","source":{"kind":"api"},
		"fields":[{"name":"uid","label":"用户","pii":"mask_middle"},{"name":"amount","label":"金额","type":"money"}],
		"card":{"title":"KYC 复核：{key}","fields":["uid","amount"]},
		"nodes":[{"id":"ops","name":"运营","assignee":{"kind":"team","team":"vip_cs_leads","mode":"claim"},"actions":["close"]}]}`)
	if _, issues, err := e.Registry.PublishBody(ctx, body, "test", e.Now()); err != nil {
		t.Fatal(err, issues)
	}
	push := func(op string, at int64, fields string) bool {
		t.Helper()
		changed, err := e.Sources.Push(ctx, source.Envelope{TaskType: "kyc_review", Key: "K1", Op: op, OccurredAt: at, Fields: []byte(fields)})
		if err != nil {
			t.Fatal(err)
		}
		return changed
	}
	if !push("upsert", 100, `{"uid":"10086","amount":"12.50"}`) {
		t.Fatal("first upsert should create")
	}
	push("upsert", 300, `{"uid":"10086","amount":"99.00"}`)
	push("upsert", 200, `{"uid":"10086","amount":"50.00"}`) // 旧消息：忽略
	tr, _ := e.Tasks.ListTracked(ctx, "kyc_review")
	if len(tr) != 1 || fmt.Sprint(tasktype.DecodeValues(tr[0].Fields)["amount"]) != "99.00" {
		t.Fatalf("tracked = %+v", tr)
	}
	if push("close", 250, "") {
		t.Fatal("stale close should be ignored")
	}
	if !push("close", 400, "") {
		t.Fatal("close should close the task")
	}
	if _, err := e.Sources.Push(ctx, source.Envelope{TaskType: "kyc_review", Key: "K2", Op: "upsert", OccurredAt: 1, Fields: []byte(`{"nope":1}`)}); !errors.Is(err, source.ErrBadEnvelope) {
		t.Fatalf("unknown field: %v", err)
	}
	if _, err := e.Sources.Push(ctx, source.Envelope{TaskType: "vip_appeal_pending", Key: "K", Op: "upsert", OccurredAt: 1}); !errors.Is(err, source.ErrBadEnvelope) {
		t.Fatalf("push to sql source: %v", err)
	}
}
