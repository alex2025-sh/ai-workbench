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

// def：rm（个人，close/snooze/escalate）→ lead（团队认领，close/no_response）→ review（团队分配，close）。
var def = &tasktype.TaskType{
	Key: "t", Priority: "P1",
	Nodes: []tasktype.Node{
		{ID: "rm", Name: "RM", Lark: true, Actions: []string{"close", "snooze", "link", "escalate"},
			Assignee: tasktype.Assignee{Kind: "relation", Relation: "vip_rm", Field: "uid", FallbackTeam: "leads"}},
		{ID: "lead", Name: "组长", Lark: true, Actions: []string{"close", "no_response", "snooze", "escalate"}, Snooze: []string{"1d"},
			Assignee: tasktype.Assignee{Kind: "team", Team: "leads", Mode: "claim"}},
		{ID: "review", Name: "审核小组", Actions: []string{"close"},
			Assignee: tasktype.Assignee{Kind: "team", Team: "review", Mode: "assign"}},
	},
}

func created(t *testing.T, to assign.Target) Task {
	t.Helper()
	res, err := Apply(def, &Task{TaskType: "t", TypeVersion: 1, BizKey: "k"}, Event{Kind: EvCreate, Now: now, Target: to})
	if err != nil {
		t.Fatal(err)
	}
	return res.Task
}

func TestCreate(t *testing.T) {
	res, _ := Apply(def, &Task{}, Event{Kind: EvCreate, Now: now, Target: assign.Target{Assignee: "alice"}})
	got := res.Task
	if got.NodeID != "rm" || got.Status != TaskStatusPending || got.AssigneeID != "alice" || !got.Tracked || got.Priority != TaskPriorityP1 || got.Version != 1 {
		t.Fatalf("task = %+v", got)
	}
	if len(res.Notify) != 1 || res.Notify[0].User != "alice" {
		t.Fatalf("notify = %+v", res.Notify)
	}
	// 关系查不到人：进入兜底团队的认领队列，通知团队。
	res, _ = Apply(def, &Task{}, Event{Kind: EvCreate, Now: now, Target: assign.Target{Team: "leads", Mode: "claim"}})
	if !res.Task.InQueue() || res.Task.TeamMode != "claim" || res.Notify[0].Team != "leads" {
		t.Fatalf("fallback = %+v %+v", res.Task, res.Notify)
	}
}

func TestTransitions(t *testing.T) {
	alice := created(t, assign.Target{Assignee: "alice"})
	queue := created(t, assign.Target{Team: "leads", Mode: "claim"})
	assignQ := created(t, assign.Target{Team: "review", Mode: "assign"})
	assignQ.NodeID = "review"
	snoozed := alice
	until := now.Add(-time.Minute)
	snoozed.Status, snoozed.SnoozeUntil = TaskStatusSnoozed, &until
	closed := alice
	closed.Status, closed.Resolution = TaskStatusSolved, ResolutionHandled

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
		{"action not enabled", alice, Event{Kind: EvAct, Actor: "alice", Action: "no_response"}, ErrState, 0, nil},
		{"link is not a transition", alice, Event{Kind: EvAct, Actor: "alice", Action: "link"}, ErrState, 0, nil},
		{"act by other", alice, Event{Kind: EvAct, Actor: "bob", Action: "close"}, ErrForbidden, 0, nil},
		{"snooze 4h", alice, Event{Kind: EvAct, Actor: "alice", Action: "snooze", Snooze: 4 * time.Hour, Now: now}, nil, TaskStatusSnoozed, func(t *testing.T, r Result) {
			if !r.Task.SnoozeUntil.Equal(now.Add(4 * time.Hour)) {
				t.Errorf("until = %v", r.Task.SnoozeUntil)
			}
		}},
		{"snooze not an option", alice, Event{Kind: EvAct, Actor: "alice", Action: "snooze", Snooze: 2 * time.Hour, Now: now}, ErrState, 0, nil},
		{"escalate to team", alice, Event{Kind: EvAct, Actor: "alice", Action: "escalate", Target: assign.Target{Team: "leads", Mode: "claim"}, ExpectNode: "rm"}, nil, TaskStatusUnassigned, func(t *testing.T, r Result) {
			if r.Task.NodeID != "lead" || r.Task.AssigneeID != "" || r.Task.TeamKey != "leads" || r.Participant != "alice" {
				t.Errorf("escalate = %+v", r.Task)
			}
			if len(r.Notify) != 1 || r.Notify[0].Team != "leads" {
				t.Errorf("notify = %+v", r.Notify)
			}
		}},
		{"escalate on stale node", alice, Event{Kind: EvAct, Actor: "alice", Action: "escalate", ExpectNode: "lead"}, ErrStale, 0, nil},
		{"claim by member", queue, Event{Kind: EvClaim, Actor: "carol", ActorRole: "executor"}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "carol" {
				t.Errorf("claim = %+v", r.Task)
			}
		}},
		{"claim by outsider", queue, Event{Kind: EvClaim, Actor: "x"}, ErrForbidden, 0, nil},
		{"claim assigned task", alice, Event{Kind: EvClaim, Actor: "carol", ActorRole: "executor"}, ErrState, 0, nil},
		{"close from queue claims it", queue, Event{Kind: EvAct, Actor: "carol", ActorRole: "executor", Action: "close", Now: now}, nil, TaskStatusSolved, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "carol" {
				t.Errorf("assignee = %q", r.Task.AssigneeID)
			}
		}},
		{"assign by assigner", assignQ, Event{Kind: EvAssign, Actor: "lead", ActorRole: "assigner", Assignee: "dave", AssigneeMember: true}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "dave" || r.Participant != "dave" {
				t.Errorf("assign = %+v", r.Task)
			}
		}},
		{"assign by executor", assignQ, Event{Kind: EvAssign, Actor: "dave", ActorRole: "executor", Assignee: "dave", AssigneeMember: true}, ErrForbidden, 0, nil},
		{"assign to outsider", assignQ, Event{Kind: EvAssign, Actor: "lead", ActorRole: "assigner", Assignee: "x"}, ErrState, 0, nil},
		{"executor cannot claim in assign mode", assignQ, Event{Kind: EvClaim, Actor: "dave", ActorRole: "executor"}, ErrForbidden, 0, nil},
		{"assigner takes it", assignQ, Event{Kind: EvClaim, Actor: "lead", ActorRole: "assigner"}, nil, TaskStatusPending, nil},
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
		{"reassign by assignee", alice, Event{Kind: EvReassign, Actor: "alice", Assignee: "bob"}, nil, TaskStatusPending, func(t *testing.T, r Result) {
			if r.Task.AssigneeID != "bob" || r.Participant != "alice" || r.Notify[0].User != "bob" {
				t.Errorf("reassign = %+v", r)
			}
		}},
		{"reassign by other", alice, Event{Kind: EvReassign, Actor: "bob", Assignee: "bob"}, ErrForbidden, 0, nil},
		{"reassign by admin", queue, Event{Kind: EvReassign, Actor: "ops", Admin: true, Assignee: "bob"}, nil, TaskStatusPending, nil},
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
