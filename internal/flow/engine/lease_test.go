package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
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

// TestPauseBeforeExecuteIsFenced：A 认领后、开始执行前停顿超过租约，B 重新认领并开始执行；
// A 恢复后发现认领已失效，不执行、不改写节点的 attempt，也不取消 B 持有的 job。
func TestPauseBeforeExecuteIsFenced(t *testing.T) {
	e := newEnv(t)
	e.publish(linear)
	releaseB := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseB) }) }
	defer release() // 回归时 A 也会卡在 skill 里，保证测试退出
	var calls atomic.Int32
	e.skills.Builtin["test.step"] = func(ctx context.Context, in json.RawMessage) (any, error) {
		calls.Add(1)
		<-releaseB
		return echo(ctx, in)
	}
	enA, enB := e.engine("A"), e.engine("B")
	paused, resumeA := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	engine.SetAfterClaim(enA, func(j engine.Job) {
		if j.Kind == engine.JobExecute && j.NodeID == "s" && once.CompareAndSwap(false, true) {
			close(paused)
			<-resumeA
		}
	})
	doneA := make(chan error, 1)
	go func() { _, err := e.startRun(enA, "biz-pause", 99); doneA <- err }()
	<-paused
	runID := e.runID("biz-pause")

	e.clock.Advance(2 * time.Minute)
	doneB := make(chan struct{})
	go func() { e.tick(enB); close(doneB) }()
	waitFor(t, func() bool { return calls.Load() == 1 }) // B 正在执行

	close(resumeA)
	select {
	case err := <-doneA:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("A executed the node after losing its claim (calls=%d)", calls.Load())
	}
	if _, nodes := nodeStatus(t, enA, runID); nodes["s"].Attempt != 2 || nodes["s"].Status != engine.NodeRunning {
		t.Fatalf("A must not touch the node: s=%s attempt=%d", nodes["s"].Status, nodes["s"].Attempt)
	}
	release()
	<-doneB

	status, nodes := nodeStatus(t, enB, runID)
	if status != engine.RunSucceeded || nodes["s"].Attempt != 2 || calls.Load() != 1 {
		t.Fatalf("run=%s s.attempt=%d calls=%d", status, nodes["s"].Attempt, calls.Load())
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

// TestLeaseCoversTimeout：节点超时长于默认租约时，开始执行会把租约延长到超时之后，执行期间不会被重复认领。
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

	e.clock.Advance(2 * time.Minute) // 超过 1 分钟的默认租约，但仍在 5 分钟超时之内
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
func (e *env) poisonRun(biz string, attempt int) (runID, jobID int64) {
	e.t.Helper()
	res, err := e.conn.ExecCtx(e.ctx, "insert into wf_run (plan_id, plan_version, biz_key, status, vars, version) values ('flow', 99, ?, 'running', '{\"payload\":{}}', 1)", biz)
	if err != nil {
		e.t.Fatal(err)
	}
	runID, _ = res.LastInsertId()
	res, err = e.conn.ExecCtx(e.ctx, "insert into wf_job (run_id, node_id, iteration, kind, due_at, status, attempt, idem_key) values (?, 's', 0, 'execute', ?, 'pending', ?, ?)",
		runID, e.clock.Now(), attempt, biz+":s")
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
	runID, jobID := e.poisonRun("biz-poison", 0)
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

// TestClaimCap：反复认领却从未完成（如处理时进程崩溃）的 job 超过认领上限后被隔离，不再执行。
func TestClaimCap(t *testing.T) {
	e := newEnv(t)
	e.publish(linear)
	en := e.engine("w1")
	runID, jobID := e.poisonRun("biz-crashloop", 30)
	e.tick(en)
	if status, fails, _ := e.jobState(jobID); status != "failed" || fails != 0 {
		t.Fatalf("job status=%s fail_count=%d", status, fails)
	}
	if e.eventCount(runID, "job_quarantined") != 1 {
		t.Fatal("missing quarantine event")
	}
}
