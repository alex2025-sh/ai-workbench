package task

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/team"
)

// 列表的范围。
const (
	ScopeMine     = "mine"     // 我名下的任务
	ScopeQueue    = "queue"    // 我所在团队的队列：认领模式下待认领、我是分配者的待分配
	ScopeInvolved = "involved" // 我经手过、现在不在我名下的任务（只读）
)

// ListQuery 是列表查询条件。
type ListQuery struct {
	Scope      string
	User       string
	Statuses   []TaskStatus
	Priorities []TaskPriority
	TaskType   string
	After      *model.TaskCursor
	Limit      int
}

// Count 是一组 (priority, status) 的任务数。
type Count struct {
	Priority TaskPriority
	Status   TaskStatus
	N        int64
}

func (s *Store) filter(ctx context.Context, q ListQuery) (model.TaskFilter, error) {
	f := model.TaskFilter{TaskType: q.TaskType, After: q.After, Limit: q.Limit}
	for _, v := range q.Statuses {
		f.Statuses = append(f.Statuses, int64(v))
	}
	for _, v := range q.Priorities {
		f.Priorities = append(f.Priorities, int64(v))
	}
	switch q.Scope {
	case ScopeQueue:
		roles, err := s.Teams.RolesOf(ctx, q.User)
		if err != nil {
			return f, err
		}
		for key, role := range roles {
			f.ClaimTeams = append(f.ClaimTeams, key)
			if role == team.RoleAssigner {
				f.AssignTeams = append(f.AssignTeams, key)
			}
		}
	case ScopeInvolved:
		f.Involved = q.User
	default:
		f.Assignee = q.User
	}
	return f, nil
}

// List 查询任务列表。
func (s *Store) List(ctx context.Context, q ListQuery) ([]Task, error) {
	f, err := s.filter(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := s.tasks.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, nil
}

// Count 按 (priority, status) 计数，条件与 List 相同。
func (s *Store) Count(ctx context.Context, q ListQuery) ([]Count, error) {
	f, err := s.filter(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := s.tasks.Count(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]Count, 0, len(rows))
	for _, r := range rows {
		out = append(out, Count{Priority: TaskPriority(r.Priority), Status: TaskStatus(r.Status), N: r.N})
	}
	return out, nil
}

// HistoryItem 是任务历史中的一条。
type HistoryItem struct {
	Kind       string
	Actor      string
	NodeID     string
	FromStatus TaskStatus
	ToStatus   TaskStatus
	Detail     json.RawMessage
	At         time.Time
}

// History 返回任务的全部历史，按发生顺序。
func (s *Store) History(ctx context.Context, id int64) ([]HistoryItem, error) {
	rows, err := s.events.ListByTask(ctx, uint64(id))
	if err != nil {
		return nil, err
	}
	out := make([]HistoryItem, 0, len(rows))
	for _, r := range rows {
		it := HistoryItem{Kind: r.Kind, Actor: r.Actor, NodeID: r.NodeId, FromStatus: TaskStatus(r.FromStatus), ToStatus: TaskStatus(r.ToStatus), At: r.CreatedAt}
		if r.Detail.Valid {
			it.Detail = json.RawMessage(r.Detail.String)
		}
		out = append(out, it)
	}
	return out, nil
}

// Access 是调用人对一个任务的权限。
type Access struct {
	View bool   // 能看：处理人、经手人、当前团队成员、管理员
	Role string // 在任务当前团队中的角色
}

// AccessOf 求调用人对任务的权限。整个流程经手过的人都能看，但只有当前处理人（或队列中能认领的人）能处理。
func (s *Store) AccessOf(ctx context.Context, t *Task, user string, admin bool) (Access, error) {
	a := Access{View: admin || (user != "" && t.AssigneeID == user)}
	if t.TeamKey != "" && user != "" {
		tm, err := s.Teams.Get(ctx, t.TeamKey)
		if err == nil {
			a.Role = tm.Role(user)
			a.View = a.View || a.Role != ""
		}
	}
	if !a.View && user != "" {
		if _, err := s.parts.FindOneByTaskIdUserId(ctx, uint64(t.ID), user); err == nil {
			a.View = true
		}
	}
	return a, nil
}
