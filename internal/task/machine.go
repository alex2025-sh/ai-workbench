package task

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// 状态转移被拒绝的原因。RPC 层把它们映射为 gRPC 状态码。
var (
	ErrForbidden = errors.New("task: not allowed for the caller")
	ErrState     = errors.New("task: not allowed in the current state")
	ErrStale     = errors.New("task: task changed, reload and retry")
)

// EventKind 是触发状态转移的事件。
type EventKind string

const (
	EvCreate   EventKind = "created"    // 数据源出现新 key
	EvStart    EventKind = "started"    // 处理人开始处理
	EvAct      EventKind = "acted"      // 节点动作：close / no_response / snooze / escalate
	EvWake     EventKind = "woke"       // 推迟到期
	EvGone     EventKind = "gone"       // 数据源里的 key 消失
	EvReassign EventKind = "reassigned" // 转派
	EvPriority EventKind = "priority"   // 调整优先级
)

// Event 是一次状态转移的输入。需要 IO 的部分（下一个节点的处理人）由调用方事先求好放进来，
// Apply 本身不读时钟、不做 IO，可以用表驱动测试覆盖全部组合。
type Event struct {
	Kind       EventKind
	Actor      string // 操作人；系统事件为空
	Now        time.Time
	Action     string        // EvAct：节点动作
	Snooze     time.Duration // EvAct snooze：推迟时长，必须是节点配置的选项之一
	Note       string
	Target     assign.Target // EvCreate、EvAct escalate：进入节点时分配给谁
	Assignee   string        // EvReassign：新处理人
	Priority   TaskPriority  // EvPriority
	Admin      bool          // 操作人有 task.admin 权限
	ExpectNode string        // 非空时要求任务仍在该节点（escalate 的处理人是按这个节点的下一个求出来的）
}

// Notify 是一次要发的通知（写入 outbox，提交后投递）。
type Notify struct {
	User string
}

// Result 是一次转移的结果。
type Result struct {
	Task        Task
	Notify      []Notify
	Participant string // 成为经手人的用户（转走后仍能只读查看）
	Detail      map[string]any
}

// Apply 计算一次状态转移。def 是任务固定的任务类型版本。
func Apply(def *tasktype.TaskType, t *Task, ev Event) (Result, error) {
	if ev.Kind == EvCreate {
		return create(def, t, ev)
	}
	cur := *t
	node, _, ok := def.Node(cur.NodeID)
	if !ok {
		return Result{}, fmt.Errorf("task %d: node %q not in %s@%d", t.ID, cur.NodeID, def.Key, t.TypeVersion)
	}
	if ev.ExpectNode != "" && ev.ExpectNode != cur.NodeID {
		return Result{}, ErrStale
	}
	if cur.Status.IsTerminal() && ev.Kind != EvGone {
		return Result{}, fmt.Errorf("%w: task is closed", ErrState)
	}
	res := Result{Detail: map[string]any{}}
	switch ev.Kind {
	case EvStart:
		if cur.Status != TaskStatusPending {
			return Result{}, fmt.Errorf("%w: only a pending task can be started", ErrState)
		}
		if cur.AssigneeID != ev.Actor {
			return Result{}, ErrForbidden
		}
		cur.Status = TaskStatusProcessing

	case EvAct:
		if !node.Has(ev.Action) || ev.Action == tasktype.ActionLink {
			return Result{}, fmt.Errorf("%w: action %q is not enabled on node %s", ErrState, ev.Action, node.ID)
		}
		if cur.AssigneeID != ev.Actor {
			return Result{}, ErrForbidden
		}
		res.Participant = ev.Actor
		res.Detail["action"] = ev.Action
		if ev.Note != "" {
			res.Detail["note"] = ev.Note
		}
		switch ev.Action {
		case tasktype.ActionClose, tasktype.ActionNoResponse:
			r := ResolutionHandled
			if ev.Action == tasktype.ActionNoResponse {
				r = ResolutionNoResponse
			}
			closeTask(&cur, r, ev.Now)
		case tasktype.ActionSnooze:
			if !allowedSnooze(node, ev.Snooze) {
				return Result{}, fmt.Errorf("%w: snooze %s is not an option of node %s", ErrState, ev.Snooze, node.ID)
			}
			until := ev.Now.Add(ev.Snooze)
			cur.Status, cur.SnoozeUntil = TaskStatusSnoozed, &until
			res.Detail["snooze_until"] = until.UnixMilli()
		case tasktype.ActionEscalate:
			next, ok := def.Next(node.ID)
			if !ok {
				return Result{}, fmt.Errorf("%w: node %s is the last node", ErrState, node.ID)
			}
			if ev.Target.Assignee == "" {
				return Result{}, fmt.Errorf("%w: nobody to escalate to", ErrState)
			}
			res.Notify = enter(&cur, next, ev.Target, res.Detail)
			res.Detail["to_node"] = next.ID
		}

	case EvWake:
		if cur.Status != TaskStatusSnoozed || cur.SnoozeUntil == nil || ev.Now.Before(*cur.SnoozeUntil) {
			return Result{}, fmt.Errorf("%w: not due", ErrState)
		}
		cur.SnoozeUntil, cur.Status = nil, TaskStatusPending
		if node.Lark && cur.AssigneeID != "" {
			res.Notify = append(res.Notify, Notify{User: cur.AssigneeID})
		}

	case EvGone:
		cur.Tracked = false
		if !cur.Status.IsTerminal() {
			closeTask(&cur, ResolutionAutoClosed, ev.Now)
		}

	case EvReassign:
		if cur.AssigneeID != ev.Actor && !ev.Admin {
			return Result{}, ErrForbidden
		}
		if ev.Assignee == "" || ev.Assignee == cur.AssigneeID {
			return Result{}, fmt.Errorf("%w: choose a different assignee", ErrState)
		}
		res.Detail["from"], res.Detail["to"] = cur.AssigneeID, ev.Assignee
		if ev.Note != "" {
			res.Detail["note"] = ev.Note
		}
		if cur.AssigneeID != "" {
			res.Participant = cur.AssigneeID
		}
		cur.AssigneeID = ev.Assignee
		if cur.Status == TaskStatusUnassigned || cur.Status == TaskStatusProcessing {
			cur.Status = TaskStatusPending
		}
		if node.Lark {
			res.Notify = append(res.Notify, Notify{User: ev.Assignee})
		}

	case EvPriority:
		if cur.AssigneeID != ev.Actor && !ev.Admin {
			return Result{}, ErrForbidden
		}
		if !ev.Priority.Valid() {
			return Result{}, fmt.Errorf("%w: invalid priority", ErrState)
		}
		res.Detail["from"], res.Detail["to"] = int(cur.Priority), int(ev.Priority)
		cur.Priority = ev.Priority

	default:
		return Result{}, fmt.Errorf("task: unknown event %q", ev.Kind)
	}
	cur.Version++
	res.Task = cur
	return res, nil
}

func create(def *tasktype.TaskType, t *Task, ev Event) (Result, error) {
	if t == nil || len(def.Nodes) == 0 {
		return Result{}, fmt.Errorf("task: create needs a task and at least one node")
	}
	if ev.Target.Assignee == "" {
		return Result{}, fmt.Errorf("%w: nobody to assign to", ErrState)
	}
	cur := *t
	cur.Tracked = true
	cur.Priority = TaskPriority(def.PriorityValue())
	res := Result{Detail: map[string]any{}}
	res.Notify = enter(&cur, def.Nodes[0], ev.Target, res.Detail)
	cur.Version = 1
	res.Task = cur
	return res, nil
}

// enter 让任务进入节点并分配给求好的处理人，节点要求时发通知。
func enter(t *Task, n tasktype.Node, to assign.Target, detail map[string]any) []Notify {
	t.NodeID, t.SnoozeUntil = n.ID, nil
	t.Status, t.AssigneeID = TaskStatusPending, to.Assignee
	detail["assignee"], detail["assigned_by"] = to.Assignee, to.By
	if n.Lark {
		return []Notify{{User: to.Assignee}}
	}
	return nil
}

func closeTask(t *Task, r Resolution, now time.Time) {
	t.Status, t.Resolution, t.SnoozeUntil = TaskStatusSolved, r, nil
	t.ClosedAt = &now
}

func allowedSnooze(n tasktype.Node, d time.Duration) bool {
	return slices.ContainsFunc(n.SnoozeOptions(), func(s string) bool {
		v, err := tasktype.ParseDuration(s)
		return err == nil && v == d
	})
}
