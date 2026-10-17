package expr

import (
	"errors"
	"reflect"
	"testing"
)

func TestAnalyze(t *testing.T) {
	cases := []struct {
		src   string
		roots []string
		nodes []string
	}{
		{`payload.vip_level > 0`, []string{"payload"}, nil},
		{`nodes.check_48h.output.appeal_status == "COMMITED"`, []string{"nodes"}, []string{"check_48h"}},
		{`string(record.uid)`, []string{"record"}, nil},
		{`record.commit_time * 1000`, []string{"record"}, nil},
		{`len(nodes.a.output.items) > 0 && nodes["b"].output.ok`, []string{"nodes"}, []string{"a", "b"}},
		{`viewer.dept in ["aml", "cs"]`, []string{"viewer"}, nil},
	}
	for _, c := range cases {
		r, err := Analyze(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if !reflect.DeepEqual(r.Roots, c.roots) || !reflect.DeepEqual(r.Nodes, c.nodes) {
			t.Errorf("%s: got roots=%v nodes=%v", c.src, r.Roots, r.Nodes)
		}
	}
}

func TestNowIsRejected(t *testing.T) {
	if _, err := Compile(`now() > payload.due`); !errors.Is(err, ErrImpure) {
		t.Fatalf("want ErrImpure, got %v", err)
	}
}

func TestSyntaxError(t *testing.T) {
	if _, err := Compile(`payload.a ==`); err == nil {
		t.Fatal("want syntax error")
	}
}

func TestRun(t *testing.T) {
	p, err := Compile(`nodes.check.output.status == "COMMITED"`)
	if err != nil {
		t.Fatal(err)
	}
	v, err := p.Run(Env{"nodes": map[string]any{"check": map[string]any{"output": map[string]any{"status": "COMMITED"}}}})
	if err != nil || v != true {
		t.Fatalf("got %v, %v", v, err)
	}
}

func TestTemplate(t *testing.T) {
	tpl, err := ParseTemplate("VIP 工单 {{ payload.id }}，等级 {{payload.level}}")
	if err != nil {
		t.Fatal(err)
	}
	s, err := tpl.Render(Env{"payload": map[string]any{"id": "A1", "level": 3}})
	if err != nil || s != "VIP 工单 A1，等级 3" {
		t.Fatalf("got %q, %v", s, err)
	}
	r, err := TemplateRefs("{{ nodes.x.output.a }} and {{ task.assignee }}")
	if err != nil || !reflect.DeepEqual(r.Nodes, []string{"x"}) || !reflect.DeepEqual(r.Roots, []string{"nodes", "task"}) {
		t.Fatalf("got %+v, %v", r, err)
	}
	if _, err := ParseTemplate("broken {{ payload.a"); err == nil {
		t.Fatal("want unclosed error")
	}
}
