package task

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/model"
)

// 列表的范围。
const (
	ScopeMine     = "mine"     // 我名下的任务
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

// CanView 报告调用人能否查看任务：当前处理人、经手人（升级或转派前处理过的人）与 task.admin。
// 只有当前处理人能处理。
func (s *Store) CanView(ctx context.Context, t *Task, user string, admin bool) (bool, error) {
	if admin || (user != "" && t.AssigneeID == user) {
		return true, nil
	}
	if user == "" {
		return false, nil
	}
	_, err := s.parts.FindOneByTaskIdUserId(ctx, uint64(t.ID), user)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
