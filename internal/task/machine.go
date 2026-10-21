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
	EvCreate   EventKind = "created"     // 数据源出现新 key
	EvClaim    EventKind = "claimed"     // 认领节点上，有人认领了待认领的任务
	EvStart    EventKind = "started"     // 处理人开始处理
	EvAct      EventKind = "acted"       // 节点动作：close / no_response / snooze / escalate
	EvTransfer EventKind = "transferred" // 转交：同一节点内换处理人，或放回待认领
	EvWake     EventKind = "woke"        // 推迟到期
	EvRemind   EventKind = "reminded"    // 待认领超过 remind_after，再提醒一次群
	EvGone     EventKind = "gone"        // 数据源里的 key 消失
	EvPriority EventKind = "priority"    // 调整优先级
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
	Target     assign.Target // EvCreate、EvAct escalate：进入节点时分配给谁（认领节点为空）
	Assignee   string        // EvTransfer：新处理人；为空表示放回待认领（只限认领节点）
	Priority   TaskPriority  // EvPriority
	Admin      bool          // 操作人有 task.admin 权限
	ExpectNode string        // 非空时要求任务仍在该节点（escalate 的处理人是按这个节点的下一个求出来的）
}

// Notify 是一次要发的通知（写入 outbox，提交后投递）。
type Notify struct {
	User string // 私聊处理人
	Chat string // 发到群（认领节点）
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

	case EvClaim:
		// 先到先得：Store.Do 锁住任务行，后到的人看到的已经不是待认领。
		if cur.Status != TaskStatusUnassigned || !node.Claim() {
			return Result{}, fmt.Errorf("%w: the task has been claimed", ErrState)
		}
		if ev.Actor == "" {
			return Result{}, ErrForbidden
		}
		cur.Status, cur.AssigneeID, cur.RemindAt = TaskStatusPending, ev.Actor, nil

	case EvAct:
		if !node.Has(ev.Action) || ev.Action == tasktype.ActionLink {
			return Result{}, fmt.Errorf("%w: action %q is not enabled on node %s", ErrState, ev.Action, node.ID)
		}
		if cur.AssigneeID == "" || cur.AssigneeID != ev.Actor {
			return Result{}, ErrForbidden // 待认领的任务先认领再处理
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
			if ev.Target.Assignee == "" && !next.Claim() {
				return Result{}, fmt.Errorf("%w: nobody to escalate to", ErrState)
			}
			res.Notify = enter(&cur, next, ev.Target, ev.Now, res.Detail)
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

	case EvRemind:
		if cur.Status != TaskStatusUnassigned || cur.RemindAt == nil || ev.Now.Before(*cur.RemindAt) {
			return Result{}, fmt.Errorf("%w: not due", ErrState)
		}
		cur.RemindAt = nil // 只再提醒一次
		res.Notify = append(res.Notify, Notify{Chat: node.Assignee.Chat})

	case EvGone:
		cur.Tracked = false
		if !cur.Status.IsTerminal() {
			closeTask(&cur, ResolutionAutoClosed, ev.Now)
		}

	case EvTransfer:
		// 当前处理人（节点启用了 transfer）或 task.admin；必须写原因。任务留在当前节点。
		if !ev.Admin && (cur.AssigneeID == "" || cur.AssigneeID != ev.Actor || !node.Has(tasktype.ActionTransfer)) {
			return Result{}, ErrForbidden
		}
		if ev.Note == "" {
			return Result{}, fmt.Errorf("%w: a reason is required", ErrState)
		}
		if ev.Assignee == cur.AssigneeID {
			return Result{}, fmt.Errorf("%w: choose a different assignee", ErrState)
		}
		if ev.Assignee == "" && !node.Claim() {
			return Result{}, fmt.Errorf("%w: only a claim node can be put back for claiming", ErrState)
		}
		res.Detail["from"], res.Detail["to"], res.Detail["note"] = cur.AssigneeID, ev.Assignee, ev.Note
		if cur.AssigneeID != "" {
			res.Participant = cur.AssigneeID
		}
		cur.SnoozeUntil = nil
		if ev.Assignee == "" {
			// 放回待认领：重新通知群，重新计算再次提醒的时间。
			res.Notify = enter(&cur, node, assign.Target{By: tasktype.AssigneeClaim}, ev.Now, map[string]any{})
		} else {
			cur.AssigneeID, cur.Status, cur.RemindAt = ev.Assignee, TaskStatusPending, nil
			if node.Lark {
				res.Notify = append(res.Notify, Notify{User: ev.Assignee})
			}
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
	if ev.Target.Assignee == "" && !def.Nodes[0].Claim() {
		return Result{}, fmt.Errorf("%w: nobody to assign to", ErrState)
	}
	cur := *t
	cur.Tracked = true
	cur.Priority = TaskPriority(def.PriorityValue())
	res := Result{Detail: map[string]any{}}
	res.Notify = enter(&cur, def.Nodes[0], ev.Target, ev.Now, res.Detail)
	cur.Version = 1
	res.Task = cur
	return res, nil
}

// enter 让任务进入节点：认领节点进入待认领并通知群；其他节点分配给求好的处理人，节点要求时私聊通知。
func enter(t *Task, n tasktype.Node, to assign.Target, now time.Time, detail map[string]any) []Notify {
	t.NodeID, t.SnoozeUntil, t.RemindAt = n.ID, nil, nil
	if n.Claim() {
		t.Status, t.AssigneeID = TaskStatusUnassigned, ""
		if d, err := tasktype.ParseDuration(n.Assignee.RemindAfter); n.Assignee.RemindAfter != "" && err == nil {
			at := now.Add(d)
			t.RemindAt = &at
		}
		detail["assigned_by"] = tasktype.AssigneeClaim
		return []Notify{{Chat: n.Assignee.Chat}}
	}
	t.Status, t.AssigneeID = TaskStatusPending, to.Assignee
	detail["assignee"], detail["assigned_by"] = to.Assignee, to.By
	if n.Lark {
		return []Notify{{User: to.Assignee}}
	}
	return nil
}

func closeTask(t *Task, r Resolution, now time.Time) {
	t.Status, t.Resolution, t.SnoozeUntil, t.RemindAt = TaskStatusSolved, r, nil, nil
	t.ClosedAt = &now
}

func allowedSnooze(n tasktype.Node, d time.Duration) bool {
	return slices.ContainsFunc(n.SnoozeOptions(), func(s string) bool {
		v, err := tasktype.ParseDuration(s)
		return err == nil && v == d
	})
}
