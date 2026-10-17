package dsl

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"30s":   30 * time.Second,
		"48h":   48 * time.Hour,
		"7d":    7 * 24 * time.Hour,
		"1d12h": 36 * time.Hour,
		"1.5d":  36 * time.Hour,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil || got.Std() != want {
			t.Errorf("%s: got %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "two days", "12h1d", "d"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
	if s := Duration(7 * 24 * time.Hour).String(); s != "7d" {
		t.Errorf("String() = %s", s)
	}
}

func TestBindingsRoundTrip(t *testing.T) {
	var cfg HumanChoiceConfig
	raw := `{"view":"rm_card","assignment":"vip_rm_policy@4","options":[{"key":"ok","label":"OK"}]}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.View.Default != "rm_card" || cfg.Assignment.Ref != "vip_rm_policy@4" || cfg.Assignment.Inline != nil {
		t.Fatalf("got %+v %+v", cfg.View, cfg.Assignment)
	}
	b, _ := json.Marshal(cfg)
	if string(b) != `{"view":"rm_card","assignment":"vip_rm_policy@4","options":[{"key":"ok","label":"OK"}]}` {
		t.Fatalf("marshal: %s", b)
	}

	raw = `{"view":{"default":"a","variants":[{"when":"viewer.dept == 'aml'","view":"b@1"}]},
	        "assignment":{"resolvers":[{"type":"group","group":"leads"}],"strategy":"claim"},
	        "options":[{"key":"ok","label":"OK"}]}`
	cfg = HumanChoiceConfig{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.View.Names(), []string{"a", "b@1"}) || cfg.Assignment.Inline.Resolvers[0].Group != "leads" {
		t.Fatalf("got %+v %+v", cfg.View, cfg.Assignment.Inline)
	}
}

func TestDecodeConfigStrict(t *testing.T) {
	if _, err := DecodeConfig(NodeTimer, json.RawMessage(`{"after":"1h","aftr":"2h"}`)); err == nil {
		t.Fatal("want unknown field error")
	}
	cfg, err := DecodeConfig(NodeLarkCard, nil)
	if err != nil || cfg.(*LarkCardConfig).View != nil {
		t.Fatalf("got %v, %v", cfg, err)
	}
}

func TestPorts(t *testing.T) {
	cases := []struct {
		n    Node
		want []string
	}{
		{Node{Type: NodeSkillCall}, []string{"next", "error"}},
		{Node{Type: NodeEnd}, nil},
		{Node{Type: NodeCondition, Config: json.RawMessage(`{"branches":[{"port":"high","when":"x"},{"port":"low","when":"y"}]}`)}, []string{"high", "low", "default"}},
		{Node{Type: NodeHumanChoice, Config: json.RawMessage(`{"options":[{"key":"ok","label":"OK"},{"key":"later","label":"L","snooze":"1h"}],"timeout":"48h"}`)}, []string{"ok", "timeout"}},
	}
	for _, c := range cases {
		got, err := Ports(c.n)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.n.Type, got, err, c.want)
		}
	}
}
