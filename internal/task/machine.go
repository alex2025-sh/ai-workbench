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
	EvClaim    EventKind = "claimed"    // 执行者认领（认领模式），或分配者领给自己
	EvAssign   EventKind = "assigned"   // 分配者指定执行者（分配模式）
	EvStart    EventKind = "started"    // 处理人开始处理
	EvAct      EventKind = "acted"      // 节点动作：close / no_response / snooze / escalate
	EvWake     EventKind = "woke"       // 推迟到期
	EvGone     EventKind = "gone"       // 数据源里的 key 消失
	EvReassign EventKind = "reassigned" // 转派
	EvPriority EventKind = "priority"   // 调整优先级
)

// Event 是一次状态转移的输入。所有需要 IO 的判断（成员关系、下一个节点的处理人）由调用方事先求好放进来，
// Apply 本身不读时钟、不做 IO，可以用表驱动测试覆盖全部组合。
type Event struct {
	Kind     EventKind
	Actor    string // 操作人；系统事件为空
	Now      time.Time
	Action   string        // EvAct：节点动作
	Snooze   time.Duration // EvAct snooze：推迟时长，必须是节点配置的选项之一
	Note     string
	Target   assign.Target // EvCreate、EvAct escalate：进入节点的去向
	Assignee string        // EvAssign、EvReassign：新处理人
	Priority TaskPriority  // EvPriority

	// 调用方求好的身份信息。
	ActorRole      string // 操作人在任务当前团队中的角色（assigner / executor），不是成员为空
	AssigneeMember bool   // EvAssign：新处理人是当前团队的成员
	Admin          bool   // 操作人有 task.admin 权限
	ExpectNode     string // 非空时要求任务仍在该节点（escalate 的处理人是按这个节点的下一个求出来的）
}

// Notify 是一次要发的通知（写入 outbox，提交后投递）。团队通知的接收人由 Store 按团队配置展开。
type Notify struct {
	User string // 发给一个人
	Team string // 发给团队：认领模式发到团队群，分配模式发给分配者
	Mode string
}

// Result 是一次转移的结果。
type Result struct {
	Task        Task
	Notify      []Notify
	Participant string // 成为经手人的用户（转走后仍能只读查看）
	Detail      map[string]any
}

// Apply 计算一次状态转移。def 是任务固定的任务类型版本；t 为 nil 只用于 EvCreate。
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
	case EvClaim:
		if !cur.InQueue() {
			return Result{}, fmt.Errorf("%w: not in a team queue", ErrState)
		}
		if !canClaim(cur, ev.ActorRole) {
			return Result{}, ErrForbidden
		}
		cur.Status, cur.AssigneeID = TaskStatusPending, ev.Actor
		res.Participant = ev.Actor

	case EvAssign:
		if !cur.InQueue() || cur.TeamMode != tasktype.ModeAssign {
			return Result{}, fmt.Errorf("%w: not waiting for an assigner", ErrState)
		}
		if ev.ActorRole != "assigner" {
			return Result{}, ErrForbidden
		}
		if ev.Assignee == "" || !ev.AssigneeMember {
			return Result{}, fmt.Errorf("%w: %s is not a member of team %s", ErrState, ev.Assignee, cur.TeamKey)
		}
		cur.Status, cur.AssigneeID = TaskStatusPending, ev.Assignee
		res.Participant = ev.Assignee
		res.Detail["assignee"] = ev.Assignee
		if node.Lark && ev.Assignee != ev.Actor {
			res.Notify = append(res.Notify, Notify{User: ev.Assignee})
		}

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
		// 任务还在队列里时，能认领的人可以直接处理，处理即认领。
		switch {
		case cur.AssigneeID == ev.Actor:
		case cur.InQueue() && canClaim(cur, ev.ActorRole):
			cur.AssigneeID = ev.Actor
		default:
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
			res.Notify = enter(&cur, next, ev.Target)
			res.Detail["to_node"] = next.ID
		}

	case EvWake:
		if cur.Status != TaskStatusSnoozed || cur.SnoozeUntil == nil || ev.Now.Before(*cur.SnoozeUntil) {
			return Result{}, fmt.Errorf("%w: not due", ErrState)
		}
		cur.SnoozeUntil = nil
		if cur.AssigneeID != "" {
			cur.Status = TaskStatusPending
			if node.Lark {
				res.Notify = append(res.Notify, Notify{User: cur.AssigneeID})
			}
		} else {
			cur.Status = TaskStatusUnassigned
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
	cur := *t
	cur.Tracked = true
	cur.Priority = TaskPriority(def.PriorityValue())
	res := Result{Notify: enter(&cur, def.Nodes[0], ev.Target), Detail: map[string]any{}}
	cur.Version = 1
	res.Task = cur
	return res, nil
}

// enter 让任务进入节点：按去向分配给人或放进团队队列，节点要求时发通知。
func enter(t *Task, n tasktype.Node, to assign.Target) []Notify {
	t.NodeID, t.SnoozeUntil = n.ID, nil
	var out []Notify
	if to.Assignee != "" {
		t.Status, t.AssigneeID, t.TeamKey, t.TeamMode = TaskStatusPending, to.Assignee, "", ""
		if n.Lark {
			out = append(out, Notify{User: to.Assignee})
		}
	} else {
		t.Status, t.AssigneeID, t.TeamKey, t.TeamMode = TaskStatusUnassigned, "", to.Team, to.Mode
		if n.Lark {
			out = append(out, Notify{Team: to.Team, Mode: to.Mode})
		}
	}
	return out
}

func closeTask(t *Task, r Resolution, now time.Time) {
	t.Status, t.Resolution, t.SnoozeUntil = TaskStatusSolved, r, nil
	t.ClosedAt = &now
}

// canClaim：认领模式下团队成员都能认领；分配模式下只有分配者能领给自己。
func canClaim(t Task, role string) bool {
	if t.TeamMode == tasktype.ModeAssign {
		return role == "assigner"
	}
	return role != ""
}

func allowedSnooze(n tasktype.Node, d time.Duration) bool {
	return slices.ContainsFunc(n.SnoozeOptions(), func(s string) bool {
		v, err := tasktype.ParseDuration(s)
		return err == nil && v == d
	})
}
