package apischema

import (
	"os"
	"strings"
	"testing"

	"github.com/alex2025-sh/ai-workbench/internal/pkg/yamljson"
)

func loadExample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../examples/task_types/" + name)
	if err != nil {
		t.Fatal(err)
	}
	j, err := yamljson.Convert(raw)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestExamplesConform(t *testing.T) {
	entries, err := os.ReadDir("../../examples/task_types")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			vs, err := ValidateTaskType(loadExample(t, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range vs {
				t.Errorf("/%s: %s", strings.Join(v.Location, "/"), v.Message)
			}
		})
	}
}

func TestViolationsPointAtField(t *testing.T) {
	body := []byte(`{"key":"x1","name":"n","schema":{"type":"object","properties":{}},
	  "workflow":{"nodes":[{"id":"start","type":"trigger"},{"id":"w","type":"timer","config":{"after":"two days"}}],"edges":[]}}`)
	vs, err := ValidateTaskType(body)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range vs {
		if strings.Join(v.Location, "/") == "workflow/nodes/1/config/after" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected violation at workflow/nodes/1/config/after, got %+v", vs)
	}
}

func TestNodeConfigSchemaCompiles(t *testing.T) {
	for typ := range nodeConfigDefs {
		raw, err := NodeConfigSchema(typ)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileDoc("https://schemas.ai-workbench/nodes/"+typ+".schema.json", raw); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
}
