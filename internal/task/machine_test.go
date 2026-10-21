package task

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

var now = time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)

// def：rm（业务关系，可转交）→ lead（人员池，不可转交）→ desk（认领，群 oc_desk，2 小时未认领再提醒）。
var def = &tasktype.TaskType{
	Key: "t", Priority: "P1",
	Nodes: []tasktype.Node{
		{ID: "rm", Name: "RM", Lark: true, Actions: []string{"close", "snooze", "link", "escalate", "transfer"},
			Assignee: tasktype.Assignee{Kind: "relation", Relation: "vip_rm", Field: "uid", Fallback: "lead"}},
		{ID: "lead", Name: "组长", Actions: []string{"close", "no_response", "snooze", "escalate"}, Snooze: []string{"1d"},
			Assignee: tasktype.Assignee{Kind: "pool", Users: []string{"carol", "dave"}}},
		{ID: "desk", Name: "值班", Lark: true, Actions: []string{"close", "transfer"},
			Assignee: tasktype.Assignee{Kind: "claim", Chat: "oc_desk", RemindAfter: "2h"}},
	},
}

func created(t *testing.T) Task {
	t.Helper()
	res, err := Apply(def, &Task{TaskType: "t", TypeVersion: 1, BizKey: "k"}, Event{Kind: EvCreate, Now: now, Target: assign.Target{Assignee: "alice", By: "relation"}})
	if err != nil {
		t.Fatal(err)
	}
	return res.Task
}

func TestCreateIntoClaimNode(t *testing.T) {
	claimFirst := &tasktype.TaskType{Key: "c", Nodes: def.Nodes[2:]}
	res, err := Apply(claimFirst, &Task{}, Event{Kind: EvCreate, Now: now, Target: assign.Target{By: "claim"}})
	if err != nil || res.Task.Status != TaskStatusUnassigned || res.Task.AssigneeID != "" || len(res.Notify) != 1 || res.Notify[0].Chat != "oc_desk" {
		t.Fatalf("create = %+v %v", res, err)
	}
}

func TestCreate(t *testing.T) {
	res, _ := Apply(def, &Task{}, Event{Kind: EvCreate, Now: now, Target: assign.Target{Assignee: "alice", By: "relation"}})
	got := res.Task
	if got.NodeID != "rm" || got.Status != TaskStatusPending || got.AssigneeID != "alice" || !got.Tracked || got.Priority != TaskPriorityP1 || got.Version != 1 {
		t.Fatalf("task = %+v", got)
	}
	if len(res.Notify) != 1 || res.Notify[0].User != "alice" || res.Detail["assigned_by"] != "relation" {
		t.Fatalf("result = %+v", res)
	}
	if _, err := Apply(def, &Task{}, Event{Kind: EvCreate, Now: now}); !errors.Is(err, ErrState) {
		t.Fatalf("create without assignee: %v", err)
	}
}

func TestTransitions(t *testing.T) {
	alice := created(t)
	lead := alice
	lead.NodeID, lead.AssigneeID = "lead", "carol"
	snoozed := alice
	until := now.Add(-time.Minute)
	snoozed.Status, snoozed.SnoozeUntil = TaskStatusSnoozed, &until
	closed := alice
	closed.Status, closed.Resolution = TaskStatusSolved, ResolutionHandled
	remindAt := now.Add(-time.Minute)
	open := alice // 认领节点上待认领
	open.NodeID, open.Status, open.AssigneeID, open.RemindAt = "desk", TaskStatusUnassigned, "", &remindAt
	desk := alice // 认领节点上已认领
	desk.NodeID, desk.AssigneeID = "desk", "erin"

	cases := []struct {
		name   string
		task   Task
		ev     Event
		err    error
		status TaskStatus
		check  func(t *testing.T, r Result)
	}{
		{"start by assignee", alice, Event{Kind: EvStart, Actor: "alice"}, nil, TaskStatusProcessing, nil},
		{"start by other", alice, Event{Kind: EvStart, Actor: "bob"}, ErrForbidden, 0, nil},
		{"close", alice, Event{Kind: EvAct, Actor: "alice", Action: "close", Now: now}, nil, TaskStatusSolved, func(t *testing.T, r Result) {
			if r.Task.Resolution != ResolutionHandled || !r.Task.Tracked || r.Task.ClosedAt == nil {
				t.Errorf("close = %+v", r.Task)
			}
		}},
		{"no_response", lead, Event{Kind: EvAct, Actor: "carol", Action: "no_response", Now: now}, nil, TaskStatusSolved, func(t *testing.T, r Result) {
			if r.Task.Resolution != ResolutionNoResponse {
				t.Errorf("resolution = %s", r.Task.Resolution)
			}
		}},
		{"action not enabled", alice, Event{Kind: EvAct, Actor: "alice", Action: "no_response"}, ErrState, 0, nil},
		{"link is not a transition", alice, Event{Kind: EvAct, Actor: "alice", Action: "link"}, ErrState, 0, nil},
		{"act by other", alice, Event{Kind: EvAct, Actor: "bob", Action: "close"}, ErrForbidden, 0, nil},
		{"snooze 4h", alice, Event{Kind: EvAct, Actor: "alice", Action: "snooze", Snooze: 4 * time.Hour, Now: now}, nil, TaskStatusSnoozed, func(t *testing.T, r Result) {
			if !r.Task.SnoozeUntil.Equal(now.Add(4 * time.Hour)) {
				t.Errorf("until = %v", r.Task.SnoozeUntil)
			}
		}},
		{"snooze not an option", lead, Event{Kind: EvAct, Actor: "carol", Action: "snooze", Snooze: 4 * time.Hour, Now: now}, ErrState, 0, nil},
		{"escalate", alice, Event{Kind: EvAct, Actor: "alice", Action: "escalate", Target: assign.Target{Assignee: "dave", By: "random"}, ExpectNode: "rm"}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.NodeID != "lead" || r.Task.AssigneeID != "dave" || r.Participant != "alice" || r.Detail["assigned_by"] != "random" {
				t.Errorf("escalate = %+v %+v", r.Task, r.Detail)
			}
			if len(r.Notify) != 0 { // lead 节点没有开 Lark
				t.Errorf("notify = %+v", r.Notify)
			}
		}},
		{"escalate without target", alice, Event{Kind: EvAct, Actor: "alice", Action: "escalate"}, ErrState, 0, nil},
		{"escalate on stale node", alice, Event{Kind: EvAct, Actor: "alice", Action: "escalate", Target: assign.Target{Assignee: "dave"}, ExpectNode: "lead"}, ErrStale, 0, nil},
		{"escalate from last node", desk, Event{Kind: EvAct, Actor: "erin", Action: "escalate"}, ErrState, 0, nil},
		{"escalate into claim node", lead, Event{Kind: EvAct, Actor: "carol", Action: "escalate", Target: assign.Target{By: "claim"}, Now: now}, nil, TaskStatusUnassigned, func(t *testing.T, r Result) {
			if r.Task.NodeID != "desk" || r.Task.AssigneeID != "" || !r.Task.RemindAt.Equal(now.Add(2*time.Hour)) {
				t.Errorf("escalate = %+v", r.Task)
			}
			if len(r.Notify) != 1 || r.Notify[0].Chat != "oc_desk" {
				t.Errorf("notify = %+v", r.Notify)
			}
		}},
		{"claim", open, Event{Kind: EvClaim, Actor: "frank"}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "frank" || r.Task.RemindAt != nil || len(r.Notify) != 0 {
				t.Errorf("claim = %+v", r)
			}
		}},
		{"claim taken", desk, Event{Kind: EvClaim, Actor: "frank"}, ErrState, 0, nil},
		{"claim on non-claim node", alice, Event{Kind: EvClaim, Actor: "frank"}, ErrState, 0, nil},
		{"act before claim", open, Event{Kind: EvAct, Actor: "frank", Action: "close"}, ErrForbidden, 0, nil},
		{"remind", open, Event{Kind: EvRemind, Now: now}, nil, TaskStatusUnassigned, func(t *testing.T, r Result) {
			if r.Task.RemindAt != nil || len(r.Notify) != 1 || r.Notify[0].Chat != "oc_desk" {
				t.Errorf("remind = %+v", r)
			}
		}},
		{"remind too early", open, Event{Kind: EvRemind, Now: now.Add(-time.Hour)}, ErrState, 0, nil},
		{"wake", snoozed, Event{Kind: EvWake, Now: now}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.SnoozeUntil != nil || len(r.Notify) != 1 || r.Notify[0].User != "alice" {
				t.Errorf("wake = %+v %+v", r.Task, r.Notify)
			}
		}},
		{"wake too early", snoozed, Event{Kind: EvWake, Now: now.Add(-time.Hour)}, ErrState, 0, nil},
		{"gone closes", alice, Event{Kind: EvGone, Now: now}, nil, TaskStatusSolved, func(t *testing.T, r Result) {
			if r.Task.Resolution != ResolutionAutoClosed || r.Task.Tracked {
				t.Errorf("gone = %+v", r.Task)
			}
		}},
		{"gone untracks closed", closed, Event{Kind: EvGone, Now: now}, nil, TaskStatusSolved, func(t *testing.T, r Result) {
			if r.Task.Resolution != ResolutionHandled || r.Task.Tracked {
				t.Errorf("gone = %+v", r.Task)
			}
		}},
		{"act on closed", closed, Event{Kind: EvAct, Actor: "alice", Action: "close"}, ErrState, 0, nil},
		{"transfer by assignee", alice, Event{Kind: EvTransfer, Actor: "alice", Assignee: "bob", Note: "休假"}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "bob" || r.Task.NodeID != "rm" || r.Participant != "alice" || r.Notify[0].User != "bob" || r.Detail["note"] != "休假" {
				t.Errorf("transfer = %+v", r)
			}
		}},
		{"transfer needs a reason", alice, Event{Kind: EvTransfer, Actor: "alice", Assignee: "bob"}, ErrState, 0, nil},
		{"transfer by other", alice, Event{Kind: EvTransfer, Actor: "bob", Assignee: "bob", Note: "x"}, ErrForbidden, 0, nil},
		{"transfer not enabled", lead, Event{Kind: EvTransfer, Actor: "carol", Assignee: "bob", Note: "x"}, ErrForbidden, 0, nil},
		{"transfer by admin", lead, Event{Kind: EvTransfer, Actor: "ops", Admin: true, Assignee: "bob", Note: "x"}, nil, TaskStatusPending, nil},
		{"release only on claim node", alice, Event{Kind: EvTransfer, Actor: "alice", Note: "x"}, ErrState, 0, nil},
		{"release back for claiming", desk, Event{Kind: EvTransfer, Actor: "erin", Note: "不熟悉", Now: now}, nil, TaskStatusUnassigned, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "" || r.Participant != "erin" || r.Notify[0].Chat != "oc_desk" || r.Task.RemindAt == nil {
				t.Errorf("release = %+v", r)
			}
		}},
		{"admin assigns an unclaimed task", open, Event{Kind: EvTransfer, Actor: "ops", Admin: true, Assignee: "bob", Note: "x"}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "bob" || r.Task.RemindAt != nil {
				t.Errorf("assign = %+v", r.Task)
			}
		}},
		{"priority", alice, Event{Kind: EvPriority, Actor: "alice", Priority: TaskPriorityP0}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.Priority != TaskPriorityP0 {
				t.Errorf("priority = %v", r.Task.Priority)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := c.task
			r, err := Apply(def, &c.task, c.ev)
			if !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if !reflect.DeepEqual(c.task, before) {
				t.Fatal("Apply modified its input")
			}
			if err != nil {
				return
			}
			if r.Task.Status != c.status {
				t.Errorf("status = %s, want %s", r.Task.Status, c.status)
			}
			if r.Task.Version != c.task.Version+1 {
				t.Errorf("version = %d", r.Task.Version)
			}
			if c.check != nil {
				c.check(t, r)
			}
		})
	}
}
