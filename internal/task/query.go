package task

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/model"
)

// DefaultPageSize 与 MaxPageSize 约束列表的每页条数。
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
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
	f := model.TaskFilter{AssigneeID: q.AssigneeID, Groups: q.Groups, TaskType: q.TaskType, Limit: pageSize(q.PageSize) + 1}
	for _, st := range q.Statuses {
		f.Statuses = append(f.Statuses, int64(st))
	}
	for _, p := range q.Priorities {
		f.Priorities = append(f.Priorities, int64(p))
	}
	if q.Cursor != "" {
		c, err := decodeCursor(q.Cursor, 3)
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
		next = encodeCursor(last.Priority, model.SortDueAt(last.DueAt).UnixMilli(), int64(last.Id))
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, next, nil
}

// Events 按写入顺序分页返回任务事件。
func (s *Store) Events(ctx context.Context, taskID int64, cursor string, size int) ([]Event, string, error) {
	limit := pageSize(size) + 1
	var after *model.TaskEventCursor
	if cursor != "" {
		c, err := decodeCursor(cursor, 2)
		if err != nil {
			return nil, "", err
		}
		after = &model.TaskEventCursor{CreatedAt: time.UnixMicro(c[0]).UTC(), ID: uint64(c[1])}
	}
	rows, err := s.events.ListByTask(ctx, uint64(taskID), after, limit)
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(rows) == limit {
		rows = rows[:len(rows)-1]
		last := rows[len(rows)-1]
		next = encodeCursor(last.CreatedAt.UnixMicro(), int64(last.Id))
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

func pageSize(n int) int {
	switch {
	case n <= 0:
		return DefaultPageSize
	case n > MaxPageSize:
		return MaxPageSize
	default:
		return n
	}
}

// 游标对调用方不透明：排序键用 | 连接后 base64。
func encodeCursor(keys ...int64) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strconv.FormatInt(k, 10)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "|")))
}

// ErrBadCursor 表示游标无法解析。
var ErrBadCursor = fmt.Errorf("task: bad cursor")

func decodeCursor(s string, n int) ([]int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrBadCursor
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != n {
		return nil, ErrBadCursor
	}
	out := make([]int64, n)
	for i, p := range parts {
		if out[i], err = strconv.ParseInt(p, 10, 64); err != nil {
			return nil, ErrBadCursor
		}
	}
	return out, nil
}
