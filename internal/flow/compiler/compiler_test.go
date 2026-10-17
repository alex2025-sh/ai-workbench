package compiler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/alex2025-sh/ai-workbench/internal/pkg/yamljson"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
)

func graph(t *testing.T, src string) dsl.Graph {
	t.Helper()
	j, err := yamljson.Convert([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var g dsl.Graph
	if err := json.Unmarshal(j, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func defaults() Defaults {
	return Defaults{
		View:       &dsl.ViewBinding{Default: "rm_card"},
		Assignment: &dsl.AssignmentBinding{Ref: "vip_rm_policy@1"},
	}
}

func codes(issues []Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, string(i.Code)+"@"+i.Path)
	}
	return out
}

func wantIssue(t *testing.T, issues []Issue, code IssueCode, pathPrefix string) {
	t.Helper()
	for _, i := range issues {
		if i.Code == code && strings.HasPrefix(i.Path, pathPrefix) {
			return
		}
	}
	t.Fatalf("want %s at %s, got %v", code, pathPrefix, codes(issues))
}

const linear = `
nodes:
  - { id: start, type: trigger }
  - { id: notify, type: lark_card }
  - { id: wait, type: timer, config: { after: 48h, anchor: payload.commit_time_ms } }
  - { id: check, type: skill_call, config: { skill: appeal.get_status, input: { id: "{{ payload.appeal_id }}" } } }
  - id: pending
    type: condition
    config: { branches: [ { port: yes, when: 'nodes.check.output.status == "COMMITED"' } ] }
  - id: ask
    type: human_choice
    config: { options: [ { key: done, label: Done }, { key: later, label: Later, snooze: 24h } ], timeout: 48h }
  - { id: ok, type: end }
  - { id: late, type: end, config: { resolution: timeout } }
edges:
  - { id: e1, from: start, port: next, to: notify }
  - { id: e2, from: notify, port: next, to: wait }
  - { id: e3, from: wait, port: next, to: check }
  - { id: e4, from: check, port: next, to: pending }
  - { id: e5, from: pending, port: yes, to: ask }
  - { id: e6, from: pending, port: default, to: ok }
  - { id: e7, from: ask, port: done, to: ok }
  - { id: e8, from: ask, port: timeout, to: late }
`

func TestCompileLinear(t *testing.T) {
	p, issues := Compile(graph(t, linear), defaults())
	if p == nil {
		t.Fatalf("unexpected issues: %v", codes(issues))
	}
	wantOrder := []string{"start", "notify", "wait", "check", "pending", "ask", "ok", "late"}
	if !reflect.DeepEqual(p.Order, wantOrder) {
		t.Errorf("order = %v, want %v", p.Order, wantOrder)
	}
	if !reflect.DeepEqual(p.Ports["ask"], []string{"done", "timeout"}) {
		t.Errorf("ask ports = %v", p.Ports["ask"])
	}
	if len(p.In["ok"]) != 2 || len(p.Out["pending"]) != 2 {
		t.Errorf("in/out = %v / %v", p.In["ok"], p.Out["pending"])
	}
	b := p.Bindings["notify"]
	if b.View.Default != "rm_card" || b.ViewFrom != FromTaskType || b.Assignment.Ref != "vip_rm_policy@1" {
		t.Errorf("notify binding = %+v", b)
	}
	if p.Bindings["ask"].ViewFrom != FromTaskType {
		t.Errorf("ask binding = %+v", p.Bindings["ask"])
	}
}

func TestCompileDeterministic(t *testing.T) {
	a, _ := Compile(graph(t, linear), defaults())
	for i := 0; i < 20; i++ {
		b, _ := Compile(graph(t, linear), defaults())
		ja, _ := MarshalPlan(a)
		jb, _ := MarshalPlan(b)
		if string(ja) != string(jb) {
			t.Fatal("plan is not deterministic")
		}
	}
}

func TestStructuralErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code IssueCode
		path string
	}{
		{"no trigger", `
nodes: [ { id: a, type: end }, { id: b, type: end } ]
edges: []`, IssueTrigger, "workflow.nodes"},
		{"two triggers", `
nodes: [ { id: a, type: trigger }, { id: b, type: trigger }, { id: z, type: end } ]
edges: [ { id: e1, from: a, port: next, to: z }, { id: e2, from: b, port: next, to: z } ]`, IssueTrigger, "workflow.nodes"},
		{"duplicate node", `
nodes: [ { id: a, type: trigger }, { id: a, type: end } ]
edges: []`, IssueDuplicateID, "workflow.nodes.a"},
		{"unknown port", `
nodes: [ { id: a, type: trigger }, { id: z, type: end } ]
edges: [ { id: e1, from: a, port: yes, to: z } ]`, IssueUnknownPort, "workflow.edges.e1.port"},
		{"missing target", `
nodes: [ { id: a, type: trigger }, { id: z, type: end } ]
edges: [ { id: e1, from: a, port: next, to: zz } ]`, IssueBadEdge, "workflow.edges.e1.to"},
		{"edge into trigger", `
nodes: [ { id: a, type: trigger }, { id: s, type: skill_call, config: { skill: x } }, { id: z, type: end } ]
edges: [ { id: e1, from: a, port: next, to: s }, { id: e2, from: s, port: next, to: a }, { id: e3, from: s, port: error, to: z } ]`, IssueBadEdge, "workflow.edges.e2.to"},
		{"unconnected port", `
nodes:
  - { id: a, type: trigger }
  - { id: c, type: condition, config: { branches: [ { port: hi, when: payload.x > 1 } ] } }
  - { id: z, type: end }
edges: [ { id: e1, from: a, port: next, to: c }, { id: e2, from: c, port: hi, to: z } ]`, IssueUnconnectedPort, "workflow.nodes.c"},
		{"unreachable", `
nodes: [ { id: a, type: trigger }, { id: z, type: end }, { id: lost, type: skill_call, config: { skill: x } } ]
edges: [ { id: e1, from: a, port: next, to: z }, { id: e2, from: lost, port: next, to: z } ]`, IssueUnreachable, "workflow.nodes.lost"},
		{"cycle", `
nodes:
  - { id: a, type: trigger }
  - { id: s1, type: skill_call, config: { skill: x } }
  - { id: s2, type: skill_call, config: { skill: x } }
edges:
  - { id: e1, from: a, port: next, to: s1 }
  - { id: e2, from: s1, port: next, to: s2 }
  - { id: e3, from: s2, port: next, to: s1 }`, IssueCycle, "workflow.edges"},
		{"bad config", `
nodes: [ { id: a, type: trigger }, { id: w, type: timer, config: { after: 1h, unit: s } }, { id: z, type: end } ]
edges: [ { id: e1, from: a, port: next, to: w }, { id: e2, from: w, port: next, to: z } ]`, IssueBadConfig, "workflow.nodes.w.config"},
		{"required field not in form", `
nodes:
  - { id: a, type: trigger }
  - { id: h, type: human_choice, config: { options: [ { key: no, label: No, required_fields: [reason] } ] } }
  - { id: z, type: end }
edges: [ { id: e1, from: a, port: next, to: h }, { id: e2, from: h, port: no, to: z } ]`, IssueBadReference, "workflow.nodes.h.config.options.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, issues := Compile(graph(t, c.src), defaults())
			if p != nil {
				t.Fatal("want compile failure")
			}
			wantIssue(t, issues, c.code, c.path)
		})
	}
}

func TestReferences(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
		code IssueCode
	}{
		{"downstream node", `{ skill: x, input: { a: "{{ nodes.z2.output.v }}" } }`, IssueBadReference},
		{"unknown node", `{ skill: x, input: { a: "{{ nodes.nope.output.v }}" } }`, IssueBadReference},
		{"self", `{ skill: x, input: { a: "{{ nodes.s.output.v }}" } }`, IssueBadReference},
		{"unknown root", `{ skill: x, input: { a: "{{ ticket.id }}" } }`, IssueBadReference},
		{"loop outside region", `{ skill: x, input: { a: "{{ loop.iteration }}" } }`, IssueBadReference},
		{"now", `{ skill: x, input: { a: "{{ now() }}" } }`, IssueBadExpression},
		{"syntax", `{ skill: x, input: { a: "{{ payload.a == }}" } }`, IssueBadExpression},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `
nodes:
  - { id: a, type: trigger }
  - { id: s, type: skill_call, config: ` + c.cfg + ` }
  - { id: z2, type: skill_call, config: { skill: y } }
  - { id: z, type: end }
edges:
  - { id: e1, from: a, port: next, to: s }
  - { id: e2, from: s, port: next, to: z2 }
  - { id: e3, from: z2, port: next, to: z }`
			_, issues := Compile(graph(t, src), defaults())
			wantIssue(t, issues, c.code, "workflow.nodes.s.config.input.a")
		})
	}

	ok := `
nodes:
  - { id: a, type: trigger }
  - { id: s, type: skill_call, config: { skill: x } }
  - id: c
    type: condition
    config: { branches: [ { port: hit, when: 'nodes.s.output.v > payload.limit && task.priority == "P0"' } ] }
  - id: n
    type: lark_card
    config: { view: { default: rm_card, variants: [ { when: 'viewer.dept == "aml"', view: aml_card@1 } ] } }
  - { id: z, type: end }
edges:
  - { id: e1, from: a, port: next, to: s }
  - { id: e2, from: s, port: next, to: c }
  - { id: e3, from: c, port: hit, to: n }
  - { id: e4, from: c, port: default, to: z }
  - { id: e5, from: n, port: next, to: z }`
	if p, issues := Compile(graph(t, ok), defaults()); p == nil {
		t.Fatalf("unexpected issues: %v", codes(issues))
	}

	viewerOutside := strings.Replace(ok, "nodes.s.output.v > payload.limit", `viewer.dept == "x"`, 1)
	_, issues := Compile(graph(t, viewerOutside), defaults())
	wantIssue(t, issues, IssueBadReference, "workflow.nodes.c.config.branches.0.when")
}

func TestInheritance(t *testing.T) {
	src := `
nodes:
  - { id: a, type: trigger }
  - id: rm
    type: human_choice
    config:
      view: rm_followup
      assignment: { resolvers: [ { type: relation, relation: vip_rm, subject: payload.uid } ] }
      options: [ { key: ok, label: OK } ]
  - id: lead
    type: human_choice
    config: { options: [ { key: ok, label: OK } ] }
  - { id: notify, type: lark_card, config: { view: done_card } }
  - { id: z, type: end }
edges:
  - { id: e1, from: a, port: next, to: rm }
  - { id: e2, from: rm, port: ok, to: lead }
  - { id: e3, from: lead, port: ok, to: notify }
  - { id: e4, from: notify, port: next, to: z }`
	p, issues := Compile(graph(t, src), defaults())
	if p == nil {
		t.Fatalf("unexpected issues: %v", codes(issues))
	}
	if b := p.Bindings["rm"]; b.ViewFrom != FromSelf || b.AssignmentFrom != FromSelf {
		t.Errorf("rm = %+v", b)
	}
	if b := p.Bindings["lead"]; b.ViewFrom != "rm" || b.View.Default != "rm_followup" || b.AssignmentFrom != "rm" {
		t.Errorf("lead = %+v", b)
	}
	if b := p.Bindings["notify"]; b.ViewFrom != FromSelf || b.AssignmentFrom != "lead" || b.Assignment.Inline == nil {
		t.Errorf("notify = %+v", b)
	}

	// 没有默认值时，最前面的节点必须显式配置。
	_, issues = Compile(graph(t, `
nodes: [ { id: a, type: trigger }, { id: n, type: lark_card }, { id: z, type: end } ]
edges: [ { id: e1, from: a, port: next, to: n }, { id: e2, from: n, port: next, to: z } ]`), Defaults{})
	wantIssue(t, issues, IssueMissingBinding, "workflow.nodes.n.config.view")
	wantIssue(t, issues, IssueMissingBinding, "workflow.nodes.n.config.to")
}

func TestInheritConflictAfterMerge(t *testing.T) {
	src := `
nodes:
  - { id: a, type: trigger }
  - { id: c, type: condition, config: { branches: [ { port: big, when: payload.amount > 100 } ] } }
  - { id: h1, type: human_choice, config: { view: card_a, options: [ { key: ok, label: OK } ] } }
  - { id: h2, type: human_choice, config: { view: card_b, options: [ { key: ok, label: OK } ] } }
  - { id: m, type: merge, config: { mode: any } }
  - { id: h3, type: human_choice, config: { options: [ { key: ok, label: OK } ] } }
  - { id: z, type: end }
edges:
  - { id: e1, from: a, port: next, to: c }
  - { id: e2, from: c, port: big, to: h1 }
  - { id: e3, from: c, port: default, to: h2 }
  - { id: e4, from: h1, port: ok, to: m }
  - { id: e5, from: h2, port: ok, to: m }
  - { id: e6, from: m, port: next, to: h3 }
  - { id: e7, from: h3, port: ok, to: z }`
	_, issues := Compile(graph(t, src), defaults())
	wantIssue(t, issues, IssueInheritConflict, "workflow.nodes.h3.config.view")
	for _, i := range issues {
		if i.Code == IssueInheritConflict && strings.HasSuffix(i.Path, "assignment") {
			t.Errorf("assignment is identical on both paths and must not conflict: %v", i)
		}
	}
}

const loop = `
nodes:
  - { id: a, type: trigger }
  - { id: supplement, type: human_choice, config: { options: [ { key: submit, label: Submit } ] } }
  - { id: enrich, type: skill_call, config: { skill: x, input: { round: "{{ loop.iteration }}" } } }
  - { id: review, type: human_choice, config: { options: [ { key: approve, label: OK }, { key: back, label: Back } ] } }
  - { id: escalate, type: lark_card }
  - { id: z, type: end }
edges:
  - { id: e1, from: a, port: next, to: supplement }
  - { id: e2, from: supplement, port: submit, to: enrich }
  - { id: e3, from: enrich, port: next, to: review }
  - { id: e4, from: review, port: approve, to: z }
  - { id: e5, from: review, port: back, to: supplement, kind: rework, max_iterations: 3, on_exhausted: escalate }
  - { id: e6, from: escalate, port: next, to: z }
`

func TestRework(t *testing.T) {
	g := graph(t, loop)
	p, issues := Compile(g, defaults())
	if p == nil {
		t.Fatalf("unexpected issues: %v", codes(issues))
	}
	if len(p.Loops) != 1 || !reflect.DeepEqual(p.Loops[0].Nodes, []string{"supplement", "enrich", "review"}) {
		t.Fatalf("loops = %+v", p.Loops)
	}
	if len(p.In["supplement"]) != 1 || len(p.Out["review"]) != 2 {
		t.Errorf("rework edge must be in Out but not In: in=%v out=%v", p.In["supplement"], p.Out["review"])
	}
}

func TestReworkErrors(t *testing.T) {
	cases := []struct {
		name    string
		replace [2]string
		code    IssueCode
		path    string
	}{
		{"exhausted inside region", [2]string{"on_exhausted: escalate", "on_exhausted: enrich"}, IssueReworkLimits, "workflow.edges.e5.on_exhausted"},
		{"exhausted missing", [2]string{"on_exhausted: escalate", "on_exhausted: nope"}, IssueReworkLimits, "workflow.edges.e5.on_exhausted"},
		{"source not human", [2]string{"from: review, port: back, to: supplement", "from: enrich, port: error, to: supplement"}, IssueReworkSource, "workflow.edges.e5.from"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := strings.Replace(loop, c.replace[0], c.replace[1], 1)
			_, issues := Compile(graph(t, src), defaults())
			wantIssue(t, issues, c.code, c.path)
		})
	}

	// 目标不支配源：supplement 与 review 之间有绕开它的路径。
	// 只要目标支配源，环区域自然只有一个入口，所以单入口校验只是防御性的。
	src := strings.Replace(loop, "  - { id: e1, from: a, port: next, to: supplement }",
		"  - { id: e1, from: a, port: next, to: supplement }\n  - { id: e0, from: a, port: next, to: review }", 1)
	_, issues := Compile(graph(t, src), defaults())
	wantIssue(t, issues, IssueReworkNotDominant, "workflow.edges.e5")
}
