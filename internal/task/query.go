package task

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/cursor"
)

// ListQuery 是任务列表的查询条件：「我的任务」用 AssigneeID，「可认领」用 Groups。
type ListQuery struct {
	AssigneeID string
	Groups     []string // 组队列中尚未分配处理人的任务
	Statuses   []TaskStatus
	TaskType   string
	Priorities []TaskPriority
	Cursor     string // 上一页返回的 next；空 = 第一页
	PageSize   int
}

// Event 是一条任务事件（状态变更、处理人、优先级等）。
type Event struct {
	ID        int64
	Type      string
	From      *TaskStatus
	To        *TaskStatus
	Actor     string
	Reason    string
	Detail    json.RawMessage
	CreatedAt time.Time
}

// List 按优先级、截止时间（空排最后）、id 排序分页查询任务，返回下一页的游标（空 = 没有更多）。
func (s *Store) List(ctx context.Context, q ListQuery) ([]Task, string, error) {
	f := model.TaskFilter{AssigneeID: q.AssigneeID, Groups: q.Groups, TaskType: q.TaskType, Limit: cursor.Size(q.PageSize) + 1}
	for _, st := range q.Statuses {
		f.Statuses = append(f.Statuses, int64(st))
	}
	for _, p := range q.Priorities {
		f.Priorities = append(f.Priorities, int64(p))
	}
	if q.Cursor != "" {
		c, err := cursor.Decode(q.Cursor, 3)
		if err != nil {
			return nil, "", err
		}
		f.After = &model.TaskCursor{Priority: c[0], DueAt: time.UnixMilli(c[1]).UTC(), ID: uint64(c[2])}
	}
	rows, err := s.tasks.List(ctx, f)
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(rows) == f.Limit {
		rows = rows[:len(rows)-1]
		last := rows[len(rows)-1]
		next = cursor.Encode(last.Priority, model.SortDueAt(last.DueAt).UnixMilli(), int64(last.Id))
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, next, nil
}

// Count 是一组 (优先级, 状态) 的任务数。
type Count struct {
	Priority TaskPriority
	Status   TaskStatus
	N        int64
}

// Count 按 (优先级, 状态) 分组计数，条件与 List 相同（忽略游标与分页）。
func (s *Store) Count(ctx context.Context, q ListQuery) ([]Count, error) {
	f := model.TaskFilter{AssigneeID: q.AssigneeID, Groups: q.Groups, TaskType: q.TaskType}
	for _, st := range q.Statuses {
		f.Statuses = append(f.Statuses, int64(st))
	}
	for _, p := range q.Priorities {
		f.Priorities = append(f.Priorities, int64(p))
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

// Events 按写入顺序分页返回任务事件。
func (s *Store) Events(ctx context.Context, taskID int64, after string, size int) ([]Event, string, error) {
	limit := cursor.Size(size) + 1
	var from *model.TaskEventCursor
	if after != "" {
		c, err := cursor.Decode(after, 2)
		if err != nil {
			return nil, "", err
		}
		from = &model.TaskEventCursor{CreatedAt: time.UnixMicro(c[0]).UTC(), ID: uint64(c[1])}
	}
	rows, err := s.events.ListByTask(ctx, uint64(taskID), from, limit)
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(rows) == limit {
		rows = rows[:len(rows)-1]
		last := rows[len(rows)-1]
		next = cursor.Encode(last.CreatedAt.UnixMicro(), int64(last.Id))
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		e := Event{ID: int64(r.Id), Type: r.Type, Actor: r.Actor, Reason: r.Reason, CreatedAt: r.CreatedAt}
		if r.FromStatus.Valid {
			v := TaskStatus(r.FromStatus.Int64)
			e.From = &v
		}
		if r.ToStatus.Valid {
			v := TaskStatus(r.ToStatus.Int64)
			e.To = &v
		}
		if r.Detail.Valid {
			e.Detail = json.RawMessage(r.Detail.String)
		}
		out = append(out, e)
	}
	return out, next, nil
}
