package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/flow/nodes"
	"github.com/alex2025-sh/ai-workbench/internal/lark"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/yamljson"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
)

// env 是一套接在临时数据库上的引擎，skill 由测试注入。
type env struct {
	t      *testing.T
	ctx    context.Context
	conn   sqlx.SqlConn
	clock  *engine.FakeClock
	skills *action.Skills
	lark   *lark.Mock
	plans  *engine.Plans
	execs  engine.Executors
	rt     *action.Runtime
}

func newEnv(t *testing.T) *env {
	dsn := testdb.New(t, "deploy/sql/*.sql")
	conn := sqlx.NewMysql(dsn)
	e := &env{t: t, ctx: context.Background(), conn: conn, clock: engine.NewFakeClock(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)),
		skills: &action.Skills{Builtin: map[string]action.SkillFunc{}}, lark: lark.NewMock(), plans: engine.NewPlans(conn), execs: nodes.Default()}
	rel := &assign.StaticRelations{}
	rel.Set("vip_rm", "u1", "rm@example.com")
	e.rt = action.NewRuntime()
	for _, a := range []action.Action{e.skills, assign.Local{Relations: rel}, e.lark, action.Logger{K: action.KindSyncTask}} {
		if err := e.rt.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// engine 创建一个引擎实例；多个实例共享同一个库，模拟多个进程。
func (e *env) engine(owner string) *engine.Engine {
	return engine.New(engine.NewStore(e.conn), e.plans, e.execs, e.rt, e.clock, engine.Options{Owner: owner})
}

// tick 模拟一次 xxl-job 的 wf-ticker：执行全部到期的 job。
func (e *env) tick(en *engine.Engine) {
	e.t.Helper()
	if _, err := en.ProcessDue(e.ctx, engine.TickOptions{}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) publish(workflow string) string {
	e.t.Helper()
	body, err := yamljson.Convert([]byte(`
key: flow
name: test flow
schema: { type: object, properties: { uid: { type: string }, n: { type: integer } } }
views: { card: { title: "card {{ payload.uid }}", fields: [ { value: "{{ payload.n }}" } ] } }
view: card
assignment: { resolvers: [ { type: relation, relation: vip_rm, subject: payload.uid } ], fallback: { group: leads } }
workflow:
` + workflow))
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := registry.PublishDirect(e.ctx, e.conn, body, "test", e.clock.Now())
	if err != nil || res.Version == 0 {
		e.t.Fatalf("publish: %v %+v", err, res)
	}
	return "flow"
}

func (e *env) start(en *engine.Engine, biz string, n int) engine.Run {
	e.t.Helper()
	run, err := e.startRun(en, biz, n)
	if err != nil {
		e.t.Fatal(err)
	}
	return run
}

// startRun 启动 run；StartRun 会就地推进到第一个等待点。可以在其他 goroutine 中调用。
func (e *env) startRun(en *engine.Engine, biz string, n int) (engine.Run, error) {
	payload, _ := json.Marshal(map[string]any{"uid": "u1", "n": n})
	run, _, err := en.StartRun(e.ctx, engine.StartRunCmd{PlanKey: "flow", PlanVersion: 1, BizKey: biz, Vars: engine.Vars{Payload: payload}})
	return run, err
}

func nodeStatus(t *testing.T, en *engine.Engine, runID int64) (engine.RunStatus, map[string]engine.NodeRun) {
	t.Helper()
	v, err := en.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]engine.NodeRun{}
	for _, n := range v.Nodes {
		m[n.NodeID] = n
	}
	return v.Run.Status, m
}

const linear = `
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.step, input: { n: "{{ payload.n }}" } } }
    - id: c
      type: condition
      config: { branches: [ { port: big, when: nodes.s.output.n > 10 } ] }
    - { id: big, type: end, config: { resolution: big } }
    - { id: small, type: end, config: { resolution: small } }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: c }
    - { id: e3, from: c, port: big, to: big }
    - { id: e4, from: c, port: default, to: small }
`

func echo(ctx context.Context, in json.RawMessage) (any, error) {
	var v map[string]any
	_ = json.Unmarshal(in, &v)
	return v, nil
}

func TestLinearAndIdempotentStart(t *testing.T) {
	e := newEnv(t)
	e.publish(linear)
	e.skills.Builtin["test.step"] = echo
	en := e.engine("w1")
	run := e.start(en, "biz-1", 42)
	again := e.start(en, "biz-1", 42)
	if again.ID != run.ID {
		t.Fatalf("same biz key must return the same run: %d vs %d", run.ID, again.ID)
	}
	// 事件驱动：StartRun 自己把自动节点跑到结束，不需要 tick。
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunSucceeded || nodes["big"].Status != engine.NodeSucceeded || nodes["small"].Status != engine.NodeSkipped {
		t.Fatalf("status=%s nodes=%+v", status, nodes)
	}
}

func TestRetryThenSucceed(t *testing.T) {
	e := newEnv(t)
	e.publish(linear)
	var calls atomic.Int32
	e.skills.Builtin["test.step"] = func(ctx context.Context, in json.RawMessage) (any, error) {
		if calls.Add(1) < 3 {
			return nil, errors.New("temporarily unavailable")
		}
		return echo(ctx, in)
	}
	en := e.engine("w1")
	run := e.start(en, "biz-retry", 1)
	if _, nodes := nodeStatus(t, en, run.ID); nodes["s"].Status != engine.NodeReady {
		t.Fatalf("after first failure s = %s", nodes["s"].Status)
	}
	// 退避：2s、4s。
	e.clock.Advance(2 * time.Second)
	e.tick(en)
	e.clock.Advance(4 * time.Second)
	e.tick(en)
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunSucceeded || calls.Load() != 3 || nodes["s"].Attempt != 3 {
		t.Fatalf("status=%s calls=%d attempt=%d", status, calls.Load(), nodes["s"].Attempt)
	}
}

func TestFailureNeedsInterventionOrErrorPort(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.broken }, retry: { max_attempts: 1 } }
    - { id: ok, type: end }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: ok }
`)
	e.skills.Builtin["test.broken"] = func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("boom") }
	en := e.engine("w1")
	run := e.start(en, "biz-fail", 1)
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunNeedsIntervention || nodes["s"].Status != engine.NodeFailed {
		t.Fatalf("status=%s s=%s", status, nodes["s"].Status)
	}
}

func TestErrorPort(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: test.unknown }, retry: { max_attempts: 2, initial_interval: 1s } }
    - { id: ok, type: end }
    - { id: fallback, type: end, config: { resolution: fallback } }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: ok }
    - { id: e3, from: s, port: error, to: fallback }
`)
	en := e.engine("w1")
	run := e.start(en, "biz-err", 1)
	e.clock.Advance(time.Second)
	e.tick(en)
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunSucceeded || nodes["fallback"].Status != engine.NodeSucceeded || nodes["ok"].Status != engine.NodeSkipped {
		t.Fatalf("status=%s nodes=%+v", status, nodes)
	}
}

func TestHumanChoiceTimeout(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - id: ask
      type: human_choice
      config: { options: [ { key: ok, label: OK } ], timeout: 48h }
    - { id: done, type: end }
    - { id: late, type: end, config: { resolution: timeout } }
  edges:
    - { id: e1, from: start, port: next, to: ask }
    - { id: e2, from: ask, port: ok, to: done }
    - { id: e3, from: ask, port: timeout, to: late }
`)
	en := e.engine("w1")
	run := e.start(en, "biz-timeout", 1)
	if _, nodes := nodeStatus(t, en, run.ID); nodes["ask"].Status != engine.NodeWaiting {
		t.Fatalf("ask = %s", nodes["ask"].Status)
	}
	sent := e.lark.Sent()
	if len(sent) != 1 || sent[0].To.Assignee != "rm@example.com" || sent[0].Card.Callback == nil {
		t.Fatalf("card = %+v", sent)
	}
	e.clock.Advance(47 * time.Hour)
	e.tick(en)
	if status, _ := nodeStatus(t, en, run.ID); status != engine.RunRunning {
		t.Fatalf("must still wait: %s", status)
	}
	e.clock.Advance(time.Hour)
	e.tick(en)
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunSucceeded || nodes["late"].Status != engine.NodeSucceeded {
		t.Fatalf("status=%s nodes=%+v", status, nodes)
	}
	// 超时后点击被拒绝。
	res, err := en.Signal(e.ctx, engine.SignalCmd{RunID: run.ID, NodeID: "ask", Option: "ok"})
	if err != nil || res.Accepted {
		t.Fatalf("late click: %+v %v", res, err)
	}
}

func TestCancel(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: wait, type: timer, config: { after: 1h } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: wait }
    - { id: e2, from: wait, port: next, to: done }
`)
	en := e.engine("w1")
	run := e.start(en, "biz-cancel", 1)
	if err := en.Cancel(e.ctx, run.ID, "ops", "test"); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(2 * time.Hour)
	e.tick(en)
	status, nodes := nodeStatus(t, en, run.ID)
	if status != engine.RunCancelled || nodes["done"].Status != engine.NodePending {
		t.Fatalf("status=%s done=%s", status, nodes["done"].Status)
	}
}

// TestZombieWorkerIsFenced：进程 A 执行超过租约，job 被进程 B 的 tick 重新认领并完成；
// A 之后提交的结果必须被丢弃，下游只被调度一次。
func TestZombieWorkerIsFenced(t *testing.T) {
	e := newEnv(t)
	e.publish(linear)
	release := make(chan struct{})
	var calls atomic.Int32
	e.skills.Builtin["test.step"] = func(ctx context.Context, in json.RawMessage) (any, error) {
		if calls.Add(1) == 1 {
			<-release // 第一次调用（进程 A，StartRun 的就地推进）卡住
		}
		return echo(ctx, in)
	}
	enA := e.engine("A")
	enB := e.engine("B")

	done := make(chan error, 1)
	go func() { _, err := e.startRun(enA, "biz-zombie", 99); done <- err }()
	for calls.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	var run engine.Run
	if err := e.conn.QueryRowCtx(e.ctx, &run.ID, "select id from wf_run where biz_key = 'biz-zombie'"); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(2 * time.Minute) // A 的租约过期
	e.tick(enB)                      // B 的 tick 重新认领并完成整个 run
	if status, _ := nodeStatus(t, enB, run.ID); status != engine.RunSucceeded {
		t.Fatalf("B should finish the run: %s", status)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	status, nodes := nodeStatus(t, enA, run.ID)
	if status != engine.RunSucceeded || nodes["s"].Attempt != 2 || nodes["big"].Status != engine.NodeSucceeded {
		t.Fatalf("status=%s s.attempt=%d", status, nodes["s"].Attempt)
	}
	var execJobs int
	if err := e.conn.QueryRowCtx(e.ctx, &execJobs, "select count(*) from wf_job where run_id = ? and node_id = 'c' and kind = 'execute'", run.ID); err != nil {
		t.Fatal(err)
	}
	if execJobs != 1 {
		t.Fatalf("downstream scheduled %d times", execJobs)
	}
}

// TestConcurrentWorkers：多个进程同时接收事件、就地推进大量 run，同时另有 tick 在跑，每个节点恰好执行一次。
func TestConcurrentWorkers(t *testing.T) {
	e := newEnv(t)
	e.publish(`
  nodes:
    - { id: start, type: trigger }
    - { id: a, type: skill_call, config: { skill: test.count } }
    - { id: b, type: skill_call, config: { skill: test.count } }
    - { id: m, type: merge }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: a }
    - { id: e2, from: start, port: next, to: b }
    - { id: e3, from: a, port: next, to: m }
    - { id: e4, from: b, port: next, to: m }
    - { id: e5, from: m, port: next, to: done }
`)
	var mu sync.Mutex
	seen := map[string]int{}
	e.skills.Builtin["test.count"] = func(context.Context, json.RawMessage) (any, error) { return map[string]any{}, nil }
	counting := &countingExecs{inner: e.execs, seen: seen, mu: &mu}
	e.execs = counting

	const runs = 30
	enA := e.engine("A")
	ids := make([]int64, runs)
	errs := make(chan error, runs)
	var wg sync.WaitGroup
	for p, owner := range []string{"A", "B", "C"} {
		en := e.engine(owner)
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := p; i < runs; i += 3 {
				run, err := e.startRun(en, fmt.Sprintf("biz-c-%d", i), i)
				if err != nil {
					errs <- err
					return
				}
				ids[i] = run.ID
			}
		}(p)
	}
	ticker := e.engine("ticker")
	stop, ticked := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(ticked)
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := ticker.ProcessDue(e.ctx, engine.TickOptions{}); err != nil {
					errs <- err
					return
				}
			}
		}
	}()
	wg.Wait()
	close(stop)
	<-ticked
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	e.tick(e.engine("final"))

	for _, id := range ids {
		if status, _ := nodeStatus(t, enA, id); status != engine.RunSucceeded {
			t.Fatalf("run %d = %s", id, status)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("%s executed %d times", k, n)
		}
	}
	if len(seen) != runs*4 { // a、b、m、done
		t.Fatalf("executions = %d", len(seen))
	}
}

type countingExecs struct {
	inner engine.Executors
	seen  map[string]int
	mu    *sync.Mutex
}

func (c *countingExecs) Get(t dsl.NodeType) (engine.Executor, bool) {
	ex, ok := c.inner.Get(t)
	if !ok {
		return nil, false
	}
	return countingExec{Executor: ex, c: c}, true
}

type countingExec struct {
	engine.Executor
	c *countingExecs
}

func (x countingExec) Execute(ctx context.Context, ec engine.ExecContext) engine.NodeResult {
	x.c.mu.Lock()
	x.c.seen[fmt.Sprintf("%d:%s", ec.Run.ID, ec.Node.ID)]++
	x.c.mu.Unlock()
	return x.Executor.Execute(ctx, ec)
}
