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

func (e *env) engineWith(owner string, opts engine.Options) *engine.Engine {
	opts.Owner = owner
	return engine.New(engine.NewStore(e.conn), e.plans, e.execs, e.rt, e.clock, opts)
}

const askThenDone = `
  nodes:
    - { id: start, type: trigger }
    - id: ask
      type: human_choice
      config: { options: [ { key: ok, label: OK } ] }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: ask }
    - { id: e2, from: ask, port: ok, to: done }
`

// TestCardSentAfterWaiting：人工节点先提交 waiting 再发卡片，处理人收到卡片时节点一定已在等待。
func TestCardSentAfterWaiting(t *testing.T) {
	e := newEnv(t)
	e.publish(askThenDone)
	en := e.engine("w1")
	var seen atomic.Value
	e.lark.BeforeSend = func(ctx context.Context, req action.Request) error {
		var in action.LarkCardInput
		_ = json.Unmarshal(req.Input, &in)
		_, nodes := nodeStatus(t, en, in.Card.Callback.RunID)
		seen.Store(nodes["ask"].Status)
		return nil
	}
	run := e.start(en, "biz-card-order", 1)
	if got := seen.Load(); got != engine.NodeWaiting {
		t.Fatalf("node status when the card was sent = %v", got)
	}
	if sent := e.lark.Sent(); len(sent) != 1 || sent[0].IdemKey == "" {
		t.Fatalf("cards = %+v", sent)
	}
	if e.effectEvents(run.ID, "effect_done", "lark_card") != 1 {
		t.Fatal("missing effect_done event")
	}
}

// effectEvents 统计某种 effect 的事件数（任务同步也是 effect，会写同类事件）。
func (e *env) effectEvents(runID int64, typ, kind string) int {
	e.t.Helper()
	var n int
	if err := e.conn.QueryRowCtx(e.ctx, &n, "select count(*) from wf_event where run_id = ? and type = ? and json_unquote(json_extract(payload, '$.kind')) = ?", runID, typ, kind); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// TestCardDeliveryRetriesThenIntervention：发卡片失败只重试发送；一直失败约 30 分钟后 run 转为人工干预。
func TestCardDeliveryRetriesThenIntervention(t *testing.T) {
	e := newEnv(t)
	e.publish(askThenDone)
	en := e.engine("w1")
	var calls atomic.Int32
	e.lark.BeforeSend = func(context.Context, action.Request) error {
		calls.Add(1)
		return errors.New("lark unavailable")
	}
	run := e.start(en, "biz-card-fail", 1)
	if status, nodes := nodeStatus(t, en, run.ID); status != engine.RunRunning || nodes["ask"].Status != engine.NodeWaiting || nodes["ask"].Attempt != 1 {
		t.Fatalf("run=%s ask=%s attempt=%d", status, nodes["ask"].Status, nodes["ask"].Attempt)
	}
	for i := 0; i < 12; i++ {
		e.clock.Advance(10 * time.Minute)
		e.tick(en)
	}
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunNeedsIntervention || calls.Load() != 10 || nodes["ask"].Attempt != 1 || e.eventCount(run.ID, "effect_failed") != 1 {
		t.Fatalf("run=%s calls=%d ask.attempt=%d", status, calls.Load(), nodes["ask"].Attempt)
	}
}

// TestCardSkippedWhenNodeDone：卡片还没发出去节点就已处理（如前端操作），重试时不再发送过期的卡片。
func TestCardSkippedWhenNodeDone(t *testing.T) {
	e := newEnv(t)
	e.publish(askThenDone)
	en := e.engine("w1")
	var fail atomic.Bool
	fail.Store(true)
	e.lark.BeforeSend = func(context.Context, action.Request) error {
		if fail.Load() {
			return errors.New("lark unavailable")
		}
		return nil
	}
	run := e.start(en, "biz-card-stale", 1)
	res, err := en.Signal(e.ctx, engine.SignalCmd{RunID: run.ID, NodeID: "ask", Option: "ok", Actor: "rm@example.com"})
	if err != nil || !res.Accepted {
		t.Fatalf("signal: %+v %v", res, err)
	}
	fail.Store(false)
	e.clock.Advance(time.Minute)
	e.tick(en)
	if sent := e.lark.Sent(); len(sent) != 0 {
		t.Fatalf("stale card sent: %+v", sent)
	}
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunSucceeded {
		t.Fatalf("run = %s", status)
	}
}

// TestParallelBranchesRunConcurrently：并行就绪的分支同时执行，不排在彼此后面。
func TestParallelBranchesRunConcurrently(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: a, type: skill_call, config: { skill: test.barrier }, retry: { max_attempts: 1 } }
    - { id: b, type: skill_call, config: { skill: test.barrier }, retry: { max_attempts: 1 } }
    - { id: m, type: merge }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: a }
    - { id: e2, from: start, port: next, to: b }
    - { id: e3, from: a, port: next, to: m }
    - { id: e4, from: b, port: next, to: m }
    - { id: e5, from: m, port: next, to: done }
`)
	var arrived atomic.Int32
	e.skills.Builtin["test.barrier"] = func(ctx context.Context, _ json.RawMessage) (any, error) {
		arrived.Add(1)
		deadline := time.Now().Add(5 * time.Second)
		for arrived.Load() < 2 {
			if time.Now().After(deadline) {
				return nil, errors.New("the other branch never started: branches ran serially")
			}
			time.Sleep(5 * time.Millisecond)
		}
		return map[string]any{}, nil
	}
	en := e.engine("w1")
	run := e.start(en, "biz-parallel", 1)
	if status, nodes := nodeStatus(t, en, run.ID); status != engine.RunSucceeded {
		t.Fatalf("run=%s a=%s b=%s", status, nodes["a"].Output, nodes["b"].Output)
	}
}

const chain = `
  nodes:
    - { id: start, type: trigger }
    - { id: s1, type: skill_call, config: { skill: test.ok } }
    - { id: s2, type: skill_call, config: { skill: test.ok } }
    - { id: s3, type: skill_call, config: { skill: test.ok } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s1 }
    - { id: e2, from: s1, port: next, to: s2 }
    - { id: e3, from: s2, port: next, to: s3 }
    - { id: e4, from: s3, port: next, to: done }
`

// TestDriveBudget：一次就地推进最多执行 DriveBudget 个 job，剩下的由 tick 接着执行。
func TestDriveBudget(t *testing.T) {
	e := newEnv(t)
	e.publish(chain)
	e.skills.Builtin["test.ok"] = func(context.Context, json.RawMessage) (any, error) { return map[string]any{}, nil }
	en := e.engineWith("w1", engine.Options{DriveBudget: 2})
	run := e.start(en, "biz-budget", 1)
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunRunning || nodes["s2"].Status != engine.NodeSucceeded || nodes["s3"].Status != engine.NodeReady {
		t.Fatalf("run=%s s2=%s s3=%s", status, nodes["s2"].Status, nodes["s3"].Status)
	}
	e.tick(en)
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunSucceeded {
		t.Fatalf("after tick run = %s", status)
	}
}

// TestDeferredStart：Defer 的启动只提交，不执行节点；随后的 tick 执行。
func TestDeferredStart(t *testing.T) {
	e := newEnv(t)
	e.publish(chain)
	var calls atomic.Int32
	e.skills.Builtin["test.ok"] = func(context.Context, json.RawMessage) (any, error) {
		calls.Add(1)
		return map[string]any{}, nil
	}
	en := e.engine("w1")
	payload, _ := json.Marshal(map[string]any{"uid": "u1", "n": 1})
	run, _, err := en.StartRun(e.ctx, engine.StartRunCmd{PlanKey: "flow", PlanVersion: 1, BizKey: "biz-defer", Vars: engine.Vars{Payload: payload}, Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("deferred start executed %d nodes", calls.Load())
	}
	e.tick(en)
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunSucceeded || calls.Load() != 3 {
		t.Fatalf("run=%s calls=%d", status, calls.Load())
	}
}

// TestAsyncSignal：Async 的信号提交后立即返回，后续节点在后台执行；Close 等待后台推进结束。
func TestAsyncSignal(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - id: ask
      type: human_choice
      config: { options: [ { key: ok, label: OK } ] }
    - { id: slow, type: skill_call, config: { skill: test.slow } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: ask }
    - { id: e2, from: ask, port: ok, to: slow }
    - { id: e3, from: slow, port: next, to: done }
`)
	release := make(chan struct{})
	var calls atomic.Int32
	e.skills.Builtin["test.slow"] = func(context.Context, json.RawMessage) (any, error) {
		calls.Add(1)
		<-release
		return map[string]any{}, nil
	}
	en := e.engine("w1")
	run := e.start(en, "biz-async", 1)

	ctx, cancel := context.WithCancel(e.ctx)
	res, err := en.Signal(ctx, engine.SignalCmd{RunID: run.ID, NodeID: "ask", Option: "ok", Actor: "rm@example.com", Async: true})
	cancel() // 请求结束；后台推进不受影响
	if err != nil || !res.Accepted {
		t.Fatalf("signal: %+v %v", res, err)
	}
	waitFor(t, func() bool { return calls.Load() == 1 }) // 信号已返回，下游仍在后台执行
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunRunning {
		t.Fatalf("run = %s", status)
	}
	close(release)
	cctx, ccancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer ccancel()
	if err := en.Close(cctx); err != nil {
		t.Fatal(err)
	}
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunSucceeded {
		t.Fatalf("after close run = %s", status)
	}
}
