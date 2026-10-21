package tasktype

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "task_types", name))
	if err != nil {
		t.Fatal(err)
	}
	body, err := FromYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestExamplesValidate(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "examples", "task_types", "*.yaml"))
	if len(files) == 0 {
		t.Fatal("no examples")
	}
	for _, f := range files {
		tt, issues := Validate(load(t, filepath.Base(f)))
		if len(issues) > 0 || tt == nil {
			t.Errorf("%s: %v", f, issues)
		}
	}
}

func TestValidateIssues(t *testing.T) {
	base := string(load(t, "vip_appeal_48h.yaml"))
	cases := []struct {
		name, from, to, path string
	}{
		{"escalate on last node", `"actions":["close","no_response","snooze"]`, `"actions":["close","escalate"]`, "nodes.1.actions"},
		{"unknown card field", `"title":"VIP 工单已等待 48 小时：{appeal_id}"`, `"title":"{nope}"`, "card.title"},
		{"link without config", `"link":{"label":"打开工单","url":"https://risk.example.com/appeal/{appeal_id}"},`, ``, "nodes.0.link"},
		{"bad cron", `"schedule":"*/1 * * * *"`, `"schedule":"every minute"`, "source.schedule"},
		{"write query", `select concat`, `delete from appeal; select concat`, "source.query"},
		{"unknown action", `"actions":["close","no_response","snooze"]`, `"actions":["close","approve"]`, "nodes.1.actions.1"},
		{"empty pool", `"users":["lead@example.com","lead.carol@example.com"]`, `"users":[]`, "nodes.1.assignee.users"},
		{"missing fallback", `"fallback":"lead@example.com",`, ``, "nodes.0.assignee"},
		{"relation field", `"field":"uid"`, `"field":"user_id"`, "nodes.0.assignee.field"},
	}
	for _, c := range cases {
		if !strings.Contains(base, c.from) {
			t.Fatalf("%s: fixture does not contain %s", c.name, c.from)
		}
		_, issues := Validate([]byte(strings.Replace(base, c.from, c.to, 1)))
		found := false
		for _, i := range issues {
			if strings.HasPrefix(i.Path, c.path) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want issue at %s, got %v", c.name, c.path, issues)
		}
	}
}

func TestSchemaMessages(t *testing.T) {
	base := string(load(t, "vip_appeal_48h.yaml"))
	_, issues := Validate([]byte(strings.Replace(base, `"fallback":"lead@example.com"`, `"fallback":""`, 1)))
	if len(issues) != 1 || issues[0].Path != "nodes.0.assignee.fallback" || issues[0].Message != "兜底人不能为空" {
		t.Fatalf("issues = %v", issues)
	}
	_, issues = Validate([]byte(strings.Replace(base, `"fallback":"lead@example.com",`, ``, 1)))
	if len(issues) != 1 || issues[0].Path != "nodes.0.assignee.fallback" || issues[0].Message != "缺少兜底人" {
		t.Fatalf("issues = %v", issues)
	}
	_, issues = Validate([]byte(strings.Replace(base, `"users":["lead@example.com","lead.carol@example.com"]`, `"users":[]`, 1)))
	if len(issues) != 1 || issues[0].Path != "nodes.1.assignee.users" || issues[0].Message != "候选人至少需要 1 项" {
		t.Fatalf("issues = %v", issues)
	}
}

func TestCheckQuery(t *testing.T) {
	ok := []string{
		"select 1 as `key`",
		"  -- comment\nSELECT a as `key` from t where b = 'update';",
		"with x as (select 1) select * from x",
	}
	for _, q := range ok {
		if err := CheckQuery(q); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
	bad := []string{"", "update t set a=1", "select 1; select 2", "select * from t for update", "select * into outfile '/tmp/x' from t"}
	for _, q := range bad {
		if err := CheckQuery(q); err == nil {
			t.Errorf("%q: want error", q)
		}
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"30m": 30 * time.Minute, "4h": 4 * time.Hour, "1d": 24 * time.Hour, "1d12h": 36 * time.Hour} {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("%s = %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "1", "h", "1y", "0h"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("%q: want error", in)
		}
	}
}

func TestRenderCard(t *testing.T) {
	tt, issues := Validate(load(t, "vip_appeal_pending.yaml"))
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	vals := DecodeValues([]byte(`{"appeal_id":"A1001","appeal_type":"withdraw_review","uid":"10000001","order_amount":"25000.00","commit_time":1760918400}`))
	c := tt.RenderCard("rm", "A1001:1760918400", vals, "http://wb/tasks/1")
	if c.Title != "VIP 工单待审核：A1001" || c.NodeName != "RM 跟进" || c.TaskURL != "http://wb/tasks/1" {
		t.Fatalf("card = %+v", c)
	}
	want := map[string]string{"工单类型": "withdraw_review", "用户 UID": "10****01", "订单金额": "25000.00", "提交时间": "2025-10-20 08:00"}
	for _, kv := range c.Fields {
		if want[kv.Label] != kv.Value {
			t.Errorf("%s = %q, want %q", kv.Label, kv.Value, want[kv.Label])
		}
	}
	if c.Link == nil || c.Link.Value != "https://risk.example.com/appeal/A1001" {
		t.Errorf("link = %+v", c.Link)
	}
}
