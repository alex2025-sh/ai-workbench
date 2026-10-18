package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
)

// TestRetryFailedNode：重试耗尽进入人工干预；修好之前重试仍失败（执行次数清零后按策略再试），修好之后流程走完。
func TestRetryFailedNode(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.flaky }, retry: { max_attempts: 1 } }
    - { id: ok, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: ok }
`)
	var fixed atomic.Bool
	e.skills.Builtin["test.flaky"] = func(context.Context, json.RawMessage) (any, error) {
		if !fixed.Load() {
			return nil, errors.New("downstream unavailable")
		}
		return map[string]any{"ok": true}, nil
	}
	en := e.engine("w1")
	run := e.start(en, "biz-retry", 1)
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunNeedsIntervention {
		t.Fatalf("status = %s", status)
	}
	if err := en.RetryNode(e.ctx, run.ID, "start", "ops", "x"); !errors.Is(err, engine.ErrNothingToRetry) {
		t.Fatalf("retry a succeeded node: %v", err)
	}
	if err := en.RetryNode(e.ctx, run.ID, "s", "ops", "first try"); err != nil {
		t.Fatal(err)
	}
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunNeedsIntervention || nodes["s"].Status != engine.NodeFailed || nodes["s"].Attempt != 1 {
		t.Fatalf("still broken: status=%s s=%+v", status, nodes["s"])
	}
	fixed.Store(true)
	if err := en.RetryNode(e.ctx, run.ID, "s", "ops", "fixed downstream"); err != nil {
		t.Fatal(err)
	}
	if status, nodes := nodeStatus(t, en, run.ID); status != engine.RunSucceeded || nodes["ok"].Status != engine.NodeSucceeded {
		t.Fatalf("status=%s nodes=%+v", status, nodes)
	}
	if err := en.RetryNode(e.ctx, run.ID, "s", "ops", "again"); !errors.Is(err, engine.ErrNotPaused) {
		t.Fatalf("retry on a finished run: %v", err)
	}
	if e.eventCount(run.ID, "node_retried") != 2 || e.eventCount(run.ID, "run_resumed") != 2 {
		t.Fatal("interventions must be recorded")
	}
}

// TestResumeRestoresTimersConsumedDuringPause：一个分支失败暂停了 run，另一个分支的定时在暂停期间到期作废；
// 恢复后定时按原到期时间补回并立即触发，汇合后流程走完。
// s 第一次失败后退避 30 分钟再失败，保证暂停时定时节点已在等待（两个分支并行执行，否则先后不定）。
func TestResumeRestoresTimersConsumedDuringPause(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.flaky }, retry: { max_attempts: 2, initial_interval: 30m, backoff_coefficient: 1 } }
    - { id: wait, type: timer, config: { after: 1h } }
    - { id: join, type: merge, config: { mode: all } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: start, port: next, to: wait }
    - { id: e3, from: s, port: next, to: join }
    - { id: e4, from: wait, port: next, to: join }
    - { id: e5, from: join, port: next, to: done }
`)
	var fixed atomic.Bool
	e.skills.Builtin["test.flaky"] = func(context.Context, json.RawMessage) (any, error) {
		if !fixed.Load() {
			return nil, errors.New("boom")
		}
		return map[string]any{}, nil
	}
	en := e.engine("w1")
	run := e.start(en, "biz-pause", 1)
	e.clock.Advance(30 * time.Minute)
	e.tick(en) // s 第二次失败，run 暂停；wait 在等待，1 小时后到期
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunNeedsIntervention {
		t.Fatalf("status = %s", status)
	}
	e.clock.Advance(time.Hour)
	e.tick(en) // 定时到期，但 run 处于暂停：定时作废，节点仍在等待
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunNeedsIntervention || nodes["wait"].Status != engine.NodeWaiting {
		t.Fatalf("status=%s wait=%s", status, nodes["wait"].Status)
	}
	fixed.Store(true)
	if err := en.RetryNode(e.ctx, run.ID, "s", "ops", "fixed"); err != nil {
		t.Fatal(err)
	}
	if status, nodes := nodeStatus(t, en, run.ID); status != engine.RunSucceeded || nodes["wait"].Status != engine.NodeSucceeded {
		t.Fatalf("status=%s nodes=%+v", status, nodes)
	}
}

// TestRetryFailedCardDelivery：卡片发送重试耗尽进入人工干预；重试人工节点重新发送卡片，之后可以正常处理。
func TestRetryFailedCardDelivery(t *testing.T) {
	e := newEnv(t)
	e.publish(askThenDone)
	en := e.engine("w1")
	var down atomic.Bool
	down.Store(true)
	e.lark.BeforeSend = func(context.Context, action.Request) error {
		if down.Load() {
			return errors.New("lark unavailable")
		}
		return nil
	}
	run := e.start(en, "biz-card-retry", 1)
	for i := 0; i < 12; i++ {
		e.clock.Advance(10 * time.Minute)
		e.tick(en)
	}
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunNeedsIntervention {
		t.Fatalf("status = %s", status)
	}
	down.Store(false)
	if err := en.RetryNode(e.ctx, run.ID, "ask", "ops", "lark recovered"); err != nil {
		t.Fatal(err)
	}
	if status, nodes := nodeStatus(t, en, run.ID); status != engine.RunRunning || nodes["ask"].Status != engine.NodeWaiting || len(e.lark.Sent()) != 1 {
		t.Fatalf("status=%s ask=%s sent=%d", status, nodes["ask"].Status, len(e.lark.Sent()))
	}
	if res, err := en.Signal(e.ctx, engine.SignalCmd{RunID: run.ID, NodeID: "ask", Option: "ok"}); err != nil || !res.Accepted {
		t.Fatalf("signal after retry: %+v %v", res, err)
	}
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunSucceeded {
		t.Fatalf("status = %s", status)
	}
}

// TestSkipNode：跳过失败的节点并指定端口，流程沿该端口继续；运行中也可以跳过等待中的人工节点。
func TestSkipNode(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.broken }, retry: { max_attempts: 1 } }
    - id: ask
      type: human_choice
      config: { options: [ { key: ok, label: OK }, { key: no, label: No } ], timeout: 24h }
    - { id: yes, type: end, config: { resolution: yes } }
    - { id: nope, type: end, config: { resolution: nope } }
    - { id: late, type: end, config: { resolution: late } }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: ask }
    - { id: e3, from: ask, port: ok, to: yes }
    - { id: e4, from: ask, port: no, to: nope }
    - { id: e5, from: ask, port: timeout, to: late }
`)
	e.skills.Builtin["test.broken"] = func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("boom") }
	en := e.engine("w1")
	run := e.start(en, "biz-skip", 1)

	if err := en.SkipNode(e.ctx, run.ID, "s", "nope", "ops", "x"); !errors.Is(err, engine.ErrInvalidPort) {
		t.Fatalf("invalid port: %v", err)
	}
	if err := en.SkipNode(e.ctx, run.ID, "start", "next", "ops", "x"); !errors.Is(err, engine.ErrNodeConcluded) {
		t.Fatalf("concluded node: %v", err)
	}
	if err := en.SkipNode(e.ctx, run.ID, "s", "next", "ops", "data fixed by hand"); err != nil {
		t.Fatal(err)
	}
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunRunning || nodes["s"].Status != engine.NodeSucceeded || nodes["ask"].Status != engine.NodeWaiting {
		t.Fatalf("after skipping s: status=%s nodes=%+v", status, nodes)
	}
	// 人工节点无人处理：运维直接指定结果；它的超时随之撤销。
	if err := en.SkipNode(e.ctx, run.ID, "ask", "no", "ops", "owner left"); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(48 * time.Hour)
	e.tick(en)
	status, nodes = nodeStatus(t, en, run.ID)
	if status != engine.RunSucceeded || nodes["nope"].Status != engine.NodeSucceeded || nodes["ask"].Port != "no" || nodes["late"].Status != engine.NodeSkipped {
		t.Fatalf("status=%s nodes=%+v", status, nodes)
	}
	if err := en.SkipNode(e.ctx, run.ID, "ask", "ok", "ops", "x"); !errors.Is(err, engine.ErrRunEnded) {
		t.Fatalf("skip on a finished run: %v", err)
	}
	if e.eventCount(run.ID, "node_skipped_manually") != 2 {
		t.Fatal("skips must be recorded")
	}
}

// TestResumeReexecutesNodeRunningDuringPause：并行分支中一个节点还在执行时另一个分支失败、run 暂停；
// 执行结果提交时被丢弃，节点停在 running。恢复后它重新执行，流程走完。
func TestResumeReexecutesNodeRunningDuringPause(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.flaky }, retry: { max_attempts: 1 } }
    - { id: slow, type: skill_call, config: { skill: test.slow } }
    - { id: join, type: merge, config: { mode: all } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: start, port: next, to: slow }
    - { id: e3, from: s, port: next, to: join }
    - { id: e4, from: slow, port: next, to: join }
    - { id: e5, from: join, port: next, to: done }
`)
	var fixed atomic.Bool
	e.skills.Builtin["test.flaky"] = func(context.Context, json.RawMessage) (any, error) {
		if !fixed.Load() {
			return nil, errors.New("boom")
		}
		return map[string]any{}, nil
	}
	release := make(chan struct{})
	var slowCalls atomic.Int32
	e.skills.Builtin["test.slow"] = func(context.Context, json.RawMessage) (any, error) {
		if slowCalls.Add(1) == 1 {
			<-release
		}
		return map[string]any{}, nil
	}
	en := e.engineWith("w1", engine.Options{DriveParallelism: 2})
	started := make(chan engine.Run)
	go func() { started <- e.start(en, "biz-running", 1) }()
	var runID int64
	for runID == 0 { // 等 s 失败、run 暂停（slow 仍在执行）
		var id int64
		_ = e.conn.QueryRowCtx(e.ctx, &id, "select id from wf_run where biz_key = 'biz-running' and status = 'needs_intervention'")
		runID = id
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	<-started
	if _, nodes := nodeStatus(t, en, runID); nodes["slow"].Status != engine.NodeRunning {
		t.Fatalf("slow = %s, want running (result fenced)", nodes["slow"].Status)
	}
	fixed.Store(true)
	if err := en.RetryNode(e.ctx, runID, "s", "ops", "fixed"); err != nil {
		t.Fatal(err)
	}
	status, nodes := nodeStatus(t, en, runID)
	if status != engine.RunSucceeded || nodes["slow"].Status != engine.NodeSucceeded || slowCalls.Load() != 2 {
		t.Fatalf("status=%s slow=%s calls=%d", status, nodes["slow"].Status, slowCalls.Load())
	}
}
