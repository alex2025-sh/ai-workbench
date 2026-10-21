package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskModel = (*customTaskModel)(nil)

type (
	// TaskModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskModel.
	TaskModel interface {
		taskModel
		WithSession(session sqlx.Session) TaskModel
		// FindOneForUpdate 在事务内锁住任务行。
		FindOneForUpdate(ctx context.Context, id uint64) (*Task, error)
		// ListTracked 返回某个任务类型全部跟踪中的任务（数据源对比用）。
		ListTracked(ctx context.Context, taskType string) ([]*Task, error)
		// UpdateFields 刷新业务字段与来源时间，不改变状态。
		UpdateFields(ctx context.Context, id uint64, fields string, sourceAt int64) error
		// DueSnoozed 返回推迟已到期的任务 id。
		DueSnoozed(ctx context.Context, now time.Time, limit int) ([]uint64, error)
		// List 按优先级（P0 在前）、id（新的在前）分页，键集分页。
		List(ctx context.Context, f TaskFilter) ([]*Task, error)
		// Count 按 (priority, status) 分组计数，条件与 List 相同（忽略 After 与 Limit）。
		Count(ctx context.Context, f TaskFilter) ([]TaskCount, error)
	}

	// TaskCount 是一组 (priority, status) 的任务数。
	TaskCount struct {
		Priority int64 `db:"priority"`
		Status   int64 `db:"status"`
		N        int64 `db:"n"`
	}

	// TaskFilter 是任务列表的条件。Assignee、Involved 二选一。
	TaskFilter struct {
		Assignee   string // 我的任务
		Involved   string // 我经手的：参与过、但现在不在我名下的任务
		Statuses   []int64
		Priorities []int64
		TaskType   string
		After      *TaskCursor
		Limit      int
	}

	// TaskCursor 是上一页最后一行的排序键。
	TaskCursor struct {
		Priority int64
		ID       uint64
	}

	customTaskModel struct {
		*defaultTaskModel
	}
)

// NewTaskModel returns a model for the database table.
func NewTaskModel(conn sqlx.SqlConn) TaskModel {
	return &customTaskModel{
		defaultTaskModel: newTaskModel(conn),
	}
}

func (m *customTaskModel) WithSession(session sqlx.Session) TaskModel {
	return NewTaskModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTaskModel) FindOneForUpdate(ctx context.Context, id uint64) (*Task, error) {
	var out Task
	err := m.conn.QueryRowCtx(ctx, &out, fmt.Sprintf("select %s from %s where `id` = ? for update", taskRows, m.table), id)
	if err == sqlx.ErrNotFound {
		return nil, ErrNotFound
	}
	return &out, err
}

func (m *customTaskModel) ListTracked(ctx context.Context, taskType string) ([]*Task, error) {
	var out []*Task
	q := fmt.Sprintf("select %s from %s where `task_type` = ? and `track_key` is not null", taskRows, m.table)
	return out, m.conn.QueryRowsCtx(ctx, &out, q, taskType)
}

func (m *customTaskModel) UpdateFields(ctx context.Context, id uint64, fields string, sourceAt int64) error {
	_, err := m.conn.ExecCtx(ctx, fmt.Sprintf("update %s set `fields` = ?, `source_at` = greatest(`source_at`, ?) where `id` = ?", m.table), fields, sourceAt, id)
	return err
}

func (m *customTaskModel) DueSnoozed(ctx context.Context, now time.Time, limit int) ([]uint64, error) {
	var out []uint64
	q := fmt.Sprintf("select `id` from %s where `status` = 2 and `snooze_until` <= ? order by `snooze_until` limit ?", m.table)
	return out, m.conn.QueryRowsCtx(ctx, &out, q, now, limit)
}

func (m *customTaskModel) List(ctx context.Context, f TaskFilter) ([]*Task, error) {
	where, args := f.where()
	if where == nil {
		return nil, nil
	}
	if f.After != nil {
		where = append(where, "(`priority` > ? or (`priority` = ? and `id` < ?))")
		args = append(args, f.After.Priority, f.After.Priority, f.After.ID)
	}
	q := fmt.Sprintf("select %s from %s where %s order by `priority`, `id` desc limit ?", taskRows, m.table, strings.Join(where, " and "))
	var out []*Task
	return out, m.conn.QueryRowsCtx(ctx, &out, q, append(args, f.Limit)...)
}

func (m *customTaskModel) Count(ctx context.Context, f TaskFilter) ([]TaskCount, error) {
	where, args := f.where()
	if where == nil {
		return nil, nil
	}
	q := fmt.Sprintf("select `priority`, `status`, count(*) as `n` from %s where %s group by `priority`, `status` order by `priority`, `status`",
		m.table, strings.Join(where, " and "))
	var out []TaskCount
	return out, m.conn.QueryRowsCtx(ctx, &out, q, args...)
}

// where 返回 List 与 Count 共用的条件；没有指定「谁」时返回 nil（结果为空）。
func (f TaskFilter) where() ([]string, []any) {
	var where []string
	var args []any
	switch {
	case f.Assignee != "":
		where, args = append(where, "`assignee_id` = ?"), append(args, f.Assignee)
	case f.Involved != "":
		where = append(where, "`id` in (select `task_id` from `task_participant` where `user_id` = ?)", "`assignee_id` <> ?")
		args = append(args, f.Involved, f.Involved)
	default:
		return nil, nil
	}
	if len(f.Statuses) > 0 {
		where = append(where, "`status` in ("+placeholders(len(f.Statuses))+")")
		for _, v := range f.Statuses {
			args = append(args, v)
		}
	}
	if len(f.Priorities) > 0 {
		where = append(where, "`priority` in ("+placeholders(len(f.Priorities))+")")
		for _, v := range f.Priorities {
			args = append(args, v)
		}
	}
	if f.TaskType != "" {
		where, args = append(where, "`task_type` = ?"), append(args, f.TaskType)
	}
	return where, args
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
