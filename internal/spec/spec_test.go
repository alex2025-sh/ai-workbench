package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../examples/task_types", name))
	if err != nil {
		t.Fatal(err)
	}
	body, err := FromYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestExamplesCompile(t *testing.T) {
	files, _ := filepath.Glob("../../examples/task_types/*.yaml")
	if len(files) == 0 {
		t.Fatal("no examples")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			res := Check(load(t, filepath.Base(f)))
			for _, i := range res.Issues {
				if i.Severity == compiler.SeverityError {
					t.Errorf("%s %s: %s", i.Code, i.Path, i.Message)
				} else {
					t.Logf("warning %s %s: %s", i.Code, i.Path, i.Message)
				}
			}
			if res.OK() && res.Plan == nil {
				t.Fatal("no plan")
			}
		})
	}
}

func TestVIPExampleBindings(t *testing.T) {
	res := Check(load(t, "vip_appeal_alert.yaml"))
	if !res.OK() {
		t.Fatalf("issues: %+v", res.Issues)
	}
	b := res.Plan.Bindings
	if b["notify_rm"].ViewFrom != compiler.FromTaskType || b["notify_rm"].AssignmentFrom != compiler.FromTaskType {
		t.Errorf("notify_rm = %+v", b["notify_rm"])
	}
	if b["remind_rm"].ViewFrom != compiler.FromSelf || b["remind_rm"].View.Default != "rm_remind_card" || b["remind_rm"].AssignmentFrom != compiler.FromTaskType {
		t.Errorf("remind_rm = %+v", b["remind_rm"])
	}
	if b["escalate"].AssignmentFrom != compiler.FromSelf || b["escalate"].Assignment.Inline.Strategy != "claim" {
		t.Errorf("escalate = %+v", b["escalate"])
	}
	if res.Spec.Priority() != "P1" {
		t.Errorf("priority = %s", res.Spec.Priority())
	}
}

func mutate(t *testing.T, old, new string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../examples/task_types/vip_appeal_alert.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, old) {
		t.Fatalf("example does not contain %q", old)
	}
	body, err := FromYAML([]byte(strings.Replace(s, old, new, 1)))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestSemanticErrors(t *testing.T) {
	cases := []struct {
		name     string
		old, new string
		code     compiler.IssueCode
		path     string
	}{
		{"schema violation uses node id", "config: { after: 48h, anchor: payload.commit_time_ms }", "config: { after: two_days, anchor: payload.commit_time_ms }",
			compiler.IssueSchema, "workflow.nodes.wait_48h.config.after"},
		{"undefined view", "config: { view: rm_remind_card }", "config: { view: rm_remind_cards }",
			compiler.IssueBadReference, "workflow.nodes.remind_rm.config.view"},
		{"mapping to unknown field", "      reviewer: record.reviewer\n", "      reviewr: record.reviewer\n",
			compiler.IssueBadReference, "sources.scan_appeal.mapping.reviewr"},
		{"payload root in mapping", "      reviewer: record.reviewer\n", "      reviewer: payload.reviewer\n",
			compiler.IssueBadReference, "sources.scan_appeal.mapping.reviewer"},
		{"bad cron", `schedule: "*/1 * * * *"`, `schedule: "*/1 * * * * *"`,
			compiler.IssueBadConfig, "sources.scan_appeal.schedule"},
		{"money must be string", "order_amount:   { type: string, title: 订单金额, x-format: money }", "order_amount:   { type: number, title: 订单金额, x-format: money }",
			compiler.IssueSchema, "schema.properties.order_amount.type"},
		{"bad pii", "x-pii: mask_middle", "x-pii: mask",
			compiler.IssueSchema, "schema.properties.uid.x-pii"},
		{"template reads unknown root", `title: "VIP 工单待审核：{{ payload.appeal_id }}"`, `title: "VIP 工单待审核：{{ ticket.id }}"`,
			compiler.IssueBadReference, "views.rm_card.title"},
		{"close_when reads payload", `close_when: record.appeal_status != "COMMITED"`, `close_when: payload.appeal_status != "COMMITED"`,
			compiler.IssueBadReference, "sources.scan_appeal.close_when"},
		{"close_when needs dedup_key", `dedup_key: "{{ payload.appeal_id }}:{{ payload.commit_time_ms }}"`, ``,
			compiler.IssueBadConfig, "sources.scan_appeal.close_when"},
		{"workflow error surfaces", "{ id: e12, from: escalate,    port: handled, to: escalated }", "{ id: e12, from: escalate,    port: done, to: escalated }",
			compiler.IssueUnknownPort, "workflow.edges.e12.port"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Check(mutate(t, c.old, c.new))
			if res.OK() {
				t.Fatal("want errors")
			}
			for _, i := range res.Issues {
				if i.Code == c.code && i.Path == c.path {
					return
				}
			}
			t.Fatalf("want %s at %s, got %+v", c.code, c.path, res.Issues)
		})
	}
}

func TestUnusedViewWarning(t *testing.T) {
	body := mutate(t, "  lead_card:\n", "  spare_card:\n    title: x\n    fields: [ { value: x } ]\n  lead_card:\n")
	res := Check(body)
	if !res.OK() {
		t.Fatalf("issues: %+v", res.Issues)
	}
	for _, i := range res.Issues {
		if i.Code == compiler.IssueUnused && i.Path == "views.spare_card" && i.Severity == compiler.SeverityWarning {
			return
		}
	}
	t.Fatalf("want unused warning, got %+v", res.Issues)
}
