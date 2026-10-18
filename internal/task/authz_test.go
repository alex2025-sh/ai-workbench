package task

import (
	"context"
	"testing"

	"github.com/alex2025-sh/ai-workbench/internal/directory"
)

func TestCanDecide(t *testing.T) {
	groups := directory.StaticGroups{"vip_cs_leads": {"lead@example.com"}}
	assigned := &Task{AssigneeID: "rm.alice@example.com"}
	queued := &Task{AssigneeGroup: "vip_cs_leads"}
	cases := []struct {
		name  string
		task  *Task
		actor string
		want  bool
	}{
		{"assignee", assigned, "rm.alice@example.com", true},
		{"other user on assigned task", assigned, "rm.bob@example.com", false},
		{"group member on assigned task", assigned, "lead@example.com", false},
		{"group member on queued task", queued, "lead@example.com", true},
		{"outsider on queued task", queued, "rm.bob@example.com", false},
		{"anonymous", assigned, "", false},
		{"no assignee and no group", &Task{}, "lead@example.com", false},
	}
	for _, c := range cases {
		got, err := CanDecide(context.Background(), c.task, c.actor, groups)
		if err != nil || got != c.want {
			t.Errorf("%s: got %v %v, want %v", c.name, got, err, c.want)
		}
	}
}
