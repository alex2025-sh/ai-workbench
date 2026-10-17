package engine

import (
	"reflect"
	"testing"

	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/yamljson"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

func compile(t *testing.T, workflow string) *compiler.Plan {
	t.Helper()
	body, err := yamljson.Convert([]byte(`
key: test_flow
name: test
schema: { type: object, properties: { amount: { type: integer } } }
view: card
views: { card: { title: t, fields: [ { value: v } ] } }
assignment: { resolvers: [ { type: group, group: g } ] }
workflow:
` + workflow))
	if err != nil {
		t.Fatal(err)
	}
	res := spec.Check(body)
	if !res.OK() {
		t.Fatalf("issues: %+v", res.Issues)
	}
	return res.Plan
}

func states(p *compiler.Plan) map[string]*state {
	st := map[string]*state{}
	for _, id := range p.Order {
		st[id] = &state{Status: NodePending}
	}
	return st
}

func succeed(p *compiler.Plan, st map[string]*state, id, port string) ([]string, []string) {
	st[id].Status, st[id].Port = NodeSucceeded, port
	return propagate(p, mergeModes(p), st, id)
}

const branchy = `
  nodes:
    - { id: start, type: trigger }
    - { id: c, type: condition, config: { branches: [ { port: big, when: payload.amount > 100 } ] } }
    - { id: a, type: skill_call, config: { skill: x } }
    - { id: b, type: skill_call, config: { skill: y } }
    - { id: after_a, type: skill_call, config: { skill: z } }
    - { id: m, type: merge }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: c }
    - { id: e2, from: c, port: big, to: a }
    - { id: e3, from: c, port: default, to: b }
    - { id: e4, from: a, port: next, to: after_a }
    - { id: e5, from: after_a, port: next, to: m }
    - { id: e6, from: b, port: next, to: m }
    - { id: e7, from: m, port: next, to: done }
`

func TestDeadPathElimination(t *testing.T) {
	p := compile(t, branchy)
	st := states(p)
	ready, _ := succeed(p, st, "start", "next")
	if !reflect.DeepEqual(ready, []string{"c"}) {
		t.Fatalf("ready = %v", ready)
	}
	// 走 default：a、after_a 被跳过，m 等 b。
	ready, skipped := succeed(p, st, "c", "default")
	if !reflect.DeepEqual(ready, []string{"b"}) || !reflect.DeepEqual(skipped, []string{"a", "after_a"}) {
		t.Fatalf("ready = %v, skipped = %v", ready, skipped)
	}
	if st["m"].Status != NodePending {
		t.Fatalf("m = %s", st["m"].Status)
	}
	ready, _ = succeed(p, st, "b", "next")
	if !reflect.DeepEqual(ready, []string{"m"}) {
		t.Fatalf("ready = %v", ready)
	}
	succeed(p, st, "m", "next")
	if st["done"].Status != NodeReady {
		t.Fatalf("done = %s", st["done"].Status)
	}
	st["done"].Status = NodeSucceeded
	if !allConcluded(p, st) {
		t.Fatal("run should be concluded")
	}
}

func TestMergeAny(t *testing.T) {
	p := compile(t, `
  nodes:
    - { id: start, type: trigger }
    - { id: a, type: skill_call, config: { skill: x } }
    - { id: b, type: skill_call, config: { skill: y } }
    - { id: m, type: merge, config: { mode: any } }
    - { id: done, type: end }
  edges:
    - { id: e1, from: start, port: next, to: a }
    - { id: e2, from: start, port: next, to: b }
    - { id: e3, from: a, port: next, to: m }
    - { id: e4, from: b, port: next, to: m }
    - { id: e5, from: m, port: next, to: done }
`)
	st := states(p)
	ready, _ := succeed(p, st, "start", "next")
	if !reflect.DeepEqual(ready, []string{"a", "b"}) {
		t.Fatalf("parallel ready = %v", ready)
	}
	ready, _ = succeed(p, st, "b", "next")
	if !reflect.DeepEqual(ready, []string{"m"}) {
		t.Fatalf("any merge should fire on first arrival: %v", ready)
	}
	ready, _ = succeed(p, st, "a", "next")
	if len(ready) != 0 {
		t.Fatalf("late arrival must be ignored: %v", ready)
	}
}

func TestErrorPortSkipsNext(t *testing.T) {
	p := compile(t, `
  nodes:
    - { id: start, type: trigger }
    - { id: s, type: skill_call, config: { skill: x } }
    - { id: ok, type: end }
    - { id: fallback, type: end, config: { resolution: failed } }
  edges:
    - { id: e1, from: start, port: next, to: s }
    - { id: e2, from: s, port: next, to: ok }
    - { id: e3, from: s, port: error, to: fallback }
`)
	st := states(p)
	succeed(p, st, "start", "next")
	ready, skipped := succeed(p, st, "s", "error")
	if !reflect.DeepEqual(ready, []string{"fallback"}) || !reflect.DeepEqual(skipped, []string{"ok"}) {
		t.Fatalf("ready = %v, skipped = %v", ready, skipped)
	}
}
