package assign

import (
	"context"
	"testing"

	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

type first struct{}

func (first) Strategy() string { return "first" }
func (first) Pick(_ context.Context, users []string, _ string) (string, error) {
	return users[0], nil
}

func TestResolve(t *testing.T) {
	rel := &StaticRelations{}
	rel.Set("vip_rm", "1001", "rm.alice@example.com")
	s := New(rel, first{})
	ctx := context.Background()
	relation := tasktype.Node{ID: "rm", Assignee: tasktype.Assignee{Kind: "relation", Relation: "vip_rm", Field: "uid", Fallback: "lead@example.com"}}

	for _, c := range []struct {
		vals    tasktype.Values
		who, by string
	}{
		{tasktype.Values{"uid": "1001"}, "rm.alice@example.com", "relation"},
		{tasktype.Values{"uid": "9999"}, "lead@example.com", "fallback"},
		{tasktype.Values{}, "lead@example.com", "fallback"},
	} {
		got, err := s.Resolve(ctx, relation, c.vals)
		if err != nil || got.Assignee != c.who || got.By != c.by {
			t.Errorf("%v: %+v %v", c.vals, got, err)
		}
	}

	pool := []string{"a@x", "b@x", "c@x"}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		got, err := s.Resolve(ctx, tasktype.Node{ID: "lead", Assignee: tasktype.Assignee{Kind: "pool", Users: pool}}, nil)
		if err != nil || got.By != "random" {
			t.Fatalf("%+v %v", got, err)
		}
		seen[got.Assignee] = true
	}
	if len(seen) != 3 {
		t.Errorf("random picked %v", seen)
	}
	got, _ := s.Resolve(ctx, tasktype.Node{ID: "lead", Assignee: tasktype.Assignee{Kind: "pool", Users: pool, Strategy: "first"}}, nil)
	if got.Assignee != "a@x" || got.By != "first" {
		t.Errorf("custom picker = %+v", got)
	}
	if _, err := s.Resolve(ctx, tasktype.Node{ID: "x", Assignee: tasktype.Assignee{Kind: "team"}}, nil); err == nil {
		t.Error("unknown kind should fail")
	}
}
