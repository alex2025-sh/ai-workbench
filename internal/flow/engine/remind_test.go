package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
)

// askWithRemind：人工节点每 24 小时提醒一次、最多 2 次，可以推迟 8 小时，72 小时超时。
const askWithRemind = `
  nodes:
    - { id: start, type: trigger }
    - id: ask
      type: human_choice
      config:
        options: [ { key: ok, label: OK }, { key: later, label: 稍后提醒, snooze: 8h } ]
        remind: { every: 24h, max: 2 }
        timeout: 72h
    - { id: done, type: end }
    - { id: late, type: end, config: { resolution: timeout } }
  edges:
    - { id: e1, from: start, port: next, to: ask }
    - { id: e2, from: ask, port: ok, to: done }
    - { id: e3, from: ask, port: timeout, to: late }
`

// startTask 启动一个带任务的 run（任务 id 非 0 才做任务同步）。
func (e *env) startTask(en *engine.Engine, biz string) engine.Run {
	e.t.Helper()
	run, _, err := en.StartRun(e.ctx, engine.StartRunCmd{PlanKey: "flow", PlanVersion: 1, BizKey: biz,
		Vars: engine.Vars{Payload: json.RawMessage(`{"uid":"u1","n":1}`), Task: engine.TaskRef{ID: 42}}})
	if err != nil {
		e.t.Fatal(err)
	}
	return run
}

// lastSync 返回 run 最近一次任务同步的目标状态与推迟到期时间。
func (e *env) lastSync(runID int64) (status string, snoozeUntil *time.Time) {
	e.t.Helper()
	var row struct {
		Payload string `db:"payload"`
	}
	if err := e.conn.QueryRowCtx(e.ctx, &row, "select payload from wf_job where run_id = ? and kind = 'effect' and json_unquote(json_extract(payload, '$.kind')) = 'sync_task' order by id desc limit 1", runID); err != nil {
		e.t.Fatal(err)
	}
	var p struct {
		Input engine.SyncPayload `json:"input"`
	}
	if err := json.Unmarshal([]byte(row.Payload), &p); err != nil {
		e.t.Fatal(err)
	}
	return p.Input.Status, p.Input.SnoozeUntil
}

// TestRemindRepeatsUpToMax：等待期间每 24 小时重发一次卡片，发满 2 次后不再提醒；超时照常计时。
func TestRemindRepeatsUpToMax(t *testing.T) {
	e := newEnv(t)
	e.publish(askWithRemind)
	en := e.engine("w1")
	run := e.start(en, "biz-remind", 1)
	for i, want := range []int{1, 1, 2, 2, 3} { // 第 0、24、48 小时各一张，之后不再提醒
		if got := len(e.lark.Sent()); got != want {
			t.Fatalf("after %dh: %d cards, want %d", i*12, got, want)
		}
		e.clock.Advance(12 * time.Hour)
		e.tick(en)
	}
	keys := map[string]bool{}
	for _, c := range e.lark.Sent() {
		keys[c.IdemKey] = true
	}
	if len(keys) != 3 || e.eventCount(run.ID, "node_reminded") != 2 {
		t.Fatalf("each reminder needs its own idempotency key: %v", keys)
	}
	e.clock.Advance(13 * time.Hour) // 第 73 小时：超时
	e.tick(en)
	if status, nodes := nodeStatus(t, en, run.ID); status != engine.RunSucceeded || nodes["late"].Status != engine.NodeSucceeded {
		t.Fatalf("status=%s nodes=%+v", status, nodes)
	}
}

// TestSnooze：推迟后任务置为 snoozed，原来的提醒作废；到期给做推迟的人重发卡片，任务回迁 pending，之后照常提醒。
func TestSnooze(t *testing.T) {
	e := newEnv(t)
	e.publish(askWithRemind)
	en := e.engine("w1")
	run := e.startTask(en, "biz-snooze")
	e.clock.Advance(20 * time.Hour)
	res, err := en.Signal(e.ctx, engine.SignalCmd{RunID: run.ID, NodeID: "ask", Option: "later", Actor: "lead@example.com"})
	if err != nil || !res.Accepted {
		t.Fatalf("snooze: %+v %v", res, err)
	}
	status, until := e.lastSync(run.ID)
	if status != engine.SyncSnoozed || until == nil || !until.Equal(e.clock.Now().Add(8*time.Hour)) {
		t.Fatalf("sync = %s until %v", status, until)
	}
	if _, nodes := nodeStatus(t, en, run.ID); nodes["ask"].Status != engine.NodeWaiting {
		t.Fatalf("ask = %s", nodes["ask"].Status)
	}

	e.clock.Advance(4 * time.Hour) // 第 24 小时：原定的提醒已作废
	e.tick(en)
	if n := len(e.lark.Sent()); n != 1 {
		t.Fatalf("reminder must be voided by the snooze: %d cards", n)
	}
	e.clock.Advance(4 * time.Hour) // 第 28 小时：推迟到期
	e.tick(en)
	sent := e.lark.Sent()
	if len(sent) != 2 || sent[1].To.Assignee != "lead@example.com" {
		t.Fatalf("cards = %+v", sent)
	}
	if status, _ := e.lastSync(run.ID); status != engine.SyncPending {
		t.Fatalf("sync after snooze = %s", status)
	}
	e.clock.Advance(24 * time.Hour) // 推迟不计入提醒次数，之后照常提醒
	e.tick(en)
	if n := len(e.lark.Sent()); n != 3 {
		t.Fatalf("reminder after snooze: %d cards", n)
	}

	res, err = en.Signal(e.ctx, engine.SignalCmd{RunID: run.ID, NodeID: "ask", Option: "ok", Actor: "lead@example.com"})
	if err != nil || !res.Accepted {
		t.Fatalf("ok: %+v %v", res, err)
	}
	var pending int
	if err := e.conn.QueryRowCtx(e.ctx, &pending, "select count(*) from wf_job where run_id = ? and status = 'pending' and kind = 'wake'", run.ID); err != nil {
		t.Fatal(err)
	}
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunSucceeded || pending != 0 {
		t.Fatalf("status=%s pending wakes=%d", status, pending)
	}
}

// TestRemindVoidedWhilePausedIsRestored：run 暂停期间到期的提醒作废；恢复后补回并立即重发。
// s 第一次失败后退避 30 分钟再失败，保证暂停时人工节点已在等待（两个分支并行执行）。
func TestRemindVoidedWhilePausedIsRestored(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.flaky }, retry: { max_attempts: 2, initial_interval: 30m, backoff_coefficient: 1 } }
    - id: ask
      type: human_choice
      config: { options: [ { key: ok, label: OK } ], remind: { every: 1h, max: 3 } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: start, port: next, to: ask }
    - { id: e3, from: s, port: next, to: done }
    - { id: e4, from: ask, port: ok, to: done }
`)
	var fixed atomic.Bool
	e.skills.Builtin["test.flaky"] = func(context.Context, json.RawMessage) (any, error) {
		if !fixed.Load() {
			return nil, errors.New("downstream unavailable")
		}
		return map[string]any{"ok": true}, nil
	}
	en := e.engine("w1")
	run := e.start(en, "biz-remind-paused", 1)
	e.clock.Advance(30 * time.Minute)
	e.tick(en)
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunNeedsIntervention {
		t.Fatalf("status = %s", status)
	}
	e.clock.Advance(time.Hour) // 第 1 小时的提醒在暂停期间到期，作废
	e.tick(en)
	if n := len(e.lark.Sent()); n != 1 {
		t.Fatalf("no reminder while paused: %d cards", n)
	}
	fixed.Store(true)
	if err := en.RetryNode(e.ctx, run.ID, "s", "ops", "fixed"); err != nil {
		t.Fatal(err)
	}
	if n := len(e.lark.Sent()); n != 2 || e.eventCount(run.ID, "node_reminded") != 1 {
		t.Fatalf("voided reminder must be restored on resume: %d cards", n)
	}
}
