package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *env) runID(biz string) int64 {
	e.t.Helper()
	var id int64
	if err := e.conn.QueryRowCtx(e.ctx, &id, "select id from wf_run where biz_key = ?", biz); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// TestSameOwnerReclaim：同一进程（owner 相同）的 tick 重新认领了自己过期的 job，旧的执行先返回。
// 旧执行只能丢弃自己的结果，不能把 job 取消掉；新执行正常提交，run 结束。
func TestSameOwnerReclaim(t *testing.T) {
	e := newEnv(t)
	e.publish(linear)
	first, second := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	e.skills.Builtin["test.step"] = func(ctx context.Context, in json.RawMessage) (any, error) {
		switch calls.Add(1) {
		case 1:
			<-first
		case 2:
			<-second
		}
		return echo(ctx, in)
	}
	en := e.engine("same")
	doneA := make(chan error, 1)
	go func() { _, err := e.startRun(en, "biz-same", 99); doneA <- err }()
	waitFor(t, func() bool { return calls.Load() == 1 })
	runID := e.runID("biz-same")

	e.clock.Advance(2 * time.Minute) // 第一次执行的租约过期
	doneB := make(chan struct{})
	go func() { e.tick(en); close(doneB) }()
	waitFor(t, func() bool { return calls.Load() == 2 })

	close(first) // 旧执行先返回
	if err := <-doneA; err != nil {
		t.Fatal(err)
	}
	close(second)
	<-doneB

	status, nodes := nodeStatus(t, en, runID)
	if status != engine.RunSucceeded || nodes["s"].Attempt != 2 || nodes["big"].Status != engine.NodeSucceeded {
		t.Fatalf("run=%s s=%s attempt=%d", status, nodes["s"].Status, nodes["s"].Attempt)
	}
}

// TestActionTimeout：skill_call 的 timeout 生效，超时按 timeout 错误码失败。
func TestActionTimeout(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.hang, timeout: 1s }, retry: { max_attempts: 1 } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: done }
`)
	e.skills.Builtin["test.hang"] = func(ctx context.Context, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	en := e.engine("w1")
	began := time.Now()
	run := e.start(en, "biz-timeout-action", 1)
	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("timeout not enforced: took %s", took)
	}
	status, nodes := nodeStatus(t, en, run.ID)
	var out struct{ Code, Error string }
	_ = json.Unmarshal(nodes["s"].Output, &out)
	if status != engine.RunNeedsIntervention || nodes["s"].Status != engine.NodeFailed || out.Code != action.CodeTimeout {
		t.Fatalf("run=%s s=%s output=%s", status, nodes["s"].Status, nodes["s"].Output)
	}
}

// TestLeaseCoversTimeout：认领时租约设为节点超时之后，慢节点执行期间不会被重复认领。
func TestLeaseCoversTimeout(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.slow, timeout: 5m } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: done }
`)
	release := make(chan struct{})
	var calls atomic.Int32
	e.skills.Builtin["test.slow"] = func(ctx context.Context, in json.RawMessage) (any, error) {
		calls.Add(1)
		<-release
		return map[string]any{}, nil
	}
	enA, enB := e.engine("A"), e.engine("B")
	doneA := make(chan error, 1)
	go func() { _, err := e.startRun(enA, "biz-slow", 1); doneA <- err }()
	waitFor(t, func() bool { return calls.Load() == 1 })

	e.clock.Advance(2 * time.Minute) // 超过默认动作超时的租约，但仍在 5 分钟超时之内
	n, err := enB.ProcessDue(e.ctx, engine.TickOptions{})
	if err != nil || n != 0 {
		t.Fatalf("B must not reclaim a job within its timeout: n=%d err=%v", n, err)
	}
	close(release)
	if err := <-doneA; err != nil {
		t.Fatal(err)
	}
	status, nodes := nodeStatus(t, enA, e.runID("biz-slow"))
	if status != engine.RunSucceeded || nodes["s"].Attempt != 1 || calls.Load() != 1 {
		t.Fatalf("run=%s attempt=%d calls=%d", status, nodes["s"].Attempt, calls.Load())
	}
}

// TestNonRetryableErrors：错误码在 non_retryable_errors 中时不重试。
func TestNonRetryableErrors(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - id: s
      type: skill_call
      config: { skill: test.bad }
      retry: { max_attempts: 3, non_retryable_errors: [bad_input] }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: done }
`)
	var calls atomic.Int32
	e.skills.Builtin["test.bad"] = func(context.Context, json.RawMessage) (any, error) {
		calls.Add(1)
		return nil, &action.Error{Code: "bad_input", Err: errors.New("appeal_id is required")}
	}
	en := e.engine("w1")
	run := e.start(en, "biz-nonretry", 1)
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunNeedsIntervention || nodes["s"].Status != engine.NodeFailed || calls.Load() != 1 {
		t.Fatalf("run=%s s=%s calls=%d", status, nodes["s"].Status, calls.Load())
	}
}

// poisonRun 插入一个引用了不存在定义版本的 run 及其 job：每次处理都会在加载定义时失败。
func (e *env) poisonRun(biz string) (runID, jobID int64) {
	e.t.Helper()
	res, err := e.conn.ExecCtx(e.ctx, "insert into wf_run (plan_id, plan_version, biz_key, status, vars, version) values ('flow', 99, ?, 'running', '{\"payload\":{}}', 1)", biz)
	if err != nil {
		e.t.Fatal(err)
	}
	runID, _ = res.LastInsertId()
	res, err = e.conn.ExecCtx(e.ctx, "insert into wf_job (run_id, node_id, iteration, kind, due_at, status, idem_key) values (?, 's', 0, 'execute', ?, 'pending', ?)",
		runID, e.clock.Now(), biz+":s")
	if err != nil {
		e.t.Fatal(err)
	}
	jobID, _ = res.LastInsertId()
	return runID, jobID
}

func (e *env) jobState(id int64) (status string, failCount int, lastError string) {
	e.t.Helper()
	var row struct {
		Status    string `db:"status"`
		FailCount int    `db:"fail_count"`
		LastError string `db:"last_error"`
	}
	if err := e.conn.QueryRowCtx(e.ctx, &row, "select status, fail_count, last_error from wf_job where id = ?", id); err != nil {
		e.t.Fatal(err)
	}
	return row.Status, row.FailCount, row.LastError
}

func (e *env) eventCount(runID int64, typ string) int {
	e.t.Helper()
	var n int
	if err := e.conn.QueryRowCtx(e.ctx, &n, "select count(*) from wf_event where run_id = ? and type = ?", runID, typ); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// TestPoisonJobQuarantined：处理总是失败的 job 退避重试并计数，连续失败 5 次后被隔离，run 转为人工干预。
func TestPoisonJobQuarantined(t *testing.T) {
	e := newEnv(t)
	e.publish(linear)
	en := e.engine("w1")
	runID, jobID := e.poisonRun("biz-poison")
	for i := 1; i <= 4; i++ {
		e.tick(en)
		if status, fails, _ := e.jobState(jobID); status != "pending" || fails != i {
			t.Fatalf("after %d ticks: status=%s fail_count=%d", i, status, fails)
		}
		e.tick(en) // 退避中，不会被再次处理
		if _, fails, _ := e.jobState(jobID); fails != i {
			t.Fatalf("job processed during backoff: fail_count=%d", fails)
		}
		e.clock.Advance(10 * time.Minute)
	}
	e.tick(en)
	status, _, lastErr := e.jobState(jobID)
	if status != "failed" || !strings.Contains(lastErr, "quarantined") {
		t.Fatalf("job status=%s last_error=%q", status, lastErr)
	}
	var runStatus string
	if err := e.conn.QueryRowCtx(e.ctx, &runStatus, "select status from wf_run where id = ?", runID); err != nil {
		t.Fatal(err)
	}
	if runStatus != string(engine.RunNeedsIntervention) || e.eventCount(runID, "job_quarantined") != 1 {
		t.Fatalf("run status=%s", runStatus)
	}
	e.clock.Advance(time.Hour)
	if n, _ := en.ProcessDue(e.ctx, engine.TickOptions{}); n != 0 {
		t.Fatalf("quarantined job processed again: %d", n)
	}
}

// crash 让 run 停在“s 已被某个进程认领、执行到一半进程崩溃”的状态：s 已执行 attempt 次，租约已过期。
func (e *env) crash(runID int64, attempt int) {
	e.t.Helper()
	if _, err := e.conn.ExecCtx(e.ctx, "update wf_node_run set status = 'running', attempt = ? where run_id = ? and node_id = 's'", attempt, runID); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.conn.ExecCtx(e.ctx, "update wf_job set owner = 'dead', attempt = ?, locked_until = ? where run_id = ? and node_id = 's' and kind = 'execute'",
		attempt, e.clock.Now().Add(-time.Second), runID); err != nil {
		e.t.Fatal(err)
	}
}

// TestLeaseExpiryCountsAsAttempt：执行中进程崩溃，租约到期后由 tick 接手并重新执行，崩溃的那次计入执行次数；
// 次数用尽时按超时失败处理，不再执行。
func TestLeaseExpiryCountsAsAttempt(t *testing.T) {
	e := newEnv(t)
	e.publish(linear) // s 是 skill_call，默认最多 5 次
	var calls atomic.Int32
	e.skills.Builtin["test.step"] = func(ctx context.Context, in json.RawMessage) (any, error) {
		calls.Add(1)
		return echo(ctx, in)
	}
	en := e.engine("w1")
	start := func(biz string) int64 {
		payload, _ := json.Marshal(map[string]any{"uid": "u1", "n": 1})
		run, _, err := en.StartRun(e.ctx, engine.StartRunCmd{PlanKey: "flow", PlanVersion: 1, BizKey: biz, Vars: engine.Vars{Payload: payload}, Defer: true})
		if err != nil {
			t.Fatal(err)
		}
		return run.ID
	}

	resumed := start("biz-crash-once")
	e.crash(resumed, 1)
	e.tick(en)
	status, nodes := nodeStatus(t, en, resumed)
	if status != engine.RunSucceeded || nodes["s"].Attempt != 2 || calls.Load() != 1 || e.eventCount(resumed, "node_lease_expired") != 1 {
		t.Fatalf("run=%s s.attempt=%d calls=%d", status, nodes["s"].Attempt, calls.Load())
	}

	exhausted := start("biz-crash-loop")
	e.crash(exhausted, 5)
	e.tick(en)
	status, nodes = nodeStatus(t, en, exhausted)
	var out struct{ Code string }
	_ = json.Unmarshal(nodes["s"].Output, &out)
	if status != engine.RunNeedsIntervention || nodes["s"].Status != engine.NodeFailed || out.Code != action.CodeTimeout || calls.Load() != 1 {
		t.Fatalf("run=%s s=%s output=%s calls=%d", status, nodes["s"].Status, nodes["s"].Output, calls.Load())
	}
}

// TestInterruptedExecutionIsRequeued：进程退出打断了正在执行的节点（调用方 ctx 取消），
// 结果仍会提交：job 放回队列、不计执行次数，下一次 tick 立即接手。
func TestInterruptedExecutionIsRequeued(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.step }, retry: { max_attempts: 1 } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: done }
`)
	var calls atomic.Int32
	e.skills.Builtin["test.step"] = func(ctx context.Context, in json.RawMessage) (any, error) {
		if calls.Add(1) == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return echo(ctx, in)
	}
	en := e.engine("w1")
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan error, 1)
	go func() {
		payload, _ := json.Marshal(map[string]any{"uid": "u1", "n": 1})
		_, _, err := en.StartRun(ctx, engine.StartRunCmd{PlanKey: "flow", PlanVersion: 1, BizKey: "biz-interrupt", Vars: engine.Vars{Payload: payload}})
		done <- err
	}()
	waitFor(t, func() bool { return calls.Load() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	runID := e.runID("biz-interrupt")
	if status, nodes := nodeStatus(t, en, runID); status != engine.RunRunning || nodes["s"].Status != engine.NodeReady || nodes["s"].Attempt != 0 {
		t.Fatalf("after interrupt run=%s s=%s attempt=%d", status, nodes["s"].Status, nodes["s"].Attempt)
	}
	e.tick(en)
	if status, nodes := nodeStatus(t, en, runID); status != engine.RunSucceeded || nodes["s"].Attempt != 1 {
		t.Fatalf("after tick run=%s s.attempt=%d", status, nodes["s"].Attempt)
	}
}
