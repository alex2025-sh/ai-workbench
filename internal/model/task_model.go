package model

import (
	"context"
	"database/sql"
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
		// ApplySync 应用引擎同步：只有 seq 更大时才写入，返回是否生效。
		ApplySync(ctx context.Context, id uint64, status int64, assignee, group, node, resolution string, snoozeUntil sql.NullTime, seq int64) (bool, error)
		// SetRun 记录驱动任务的 run（只在尚未设置时写入）。
		SetRun(ctx context.Context, id, runID uint64) error
		// CloseUnstarted 关闭尚未启动 run、尚未完结的任务。
		CloseUnstarted(ctx context.Context, id uint64, status int64, resolution string) error
		// SetAssignment 改变处理人与状态（认领、开始处理、转派），在锁住任务行的事务内调用。
		SetAssignment(ctx context.Context, id uint64, status int64, assignee, group string) error
		// SetPriority 调整优先级，在锁住任务行的事务内调用。
		SetPriority(ctx context.Context, id uint64, priority int64) error
		// List 按优先级、截止时间（空排最后）、id 排序分页查询，键集分页。
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

	// TaskFilter 是任务列表的查询条件。AssigneeID 与 Groups 至少指定一个；同时指定时取并集。
	TaskFilter struct {
		AssigneeID string
		Groups     []string // 组队列中尚未分配处理人的任务
		Statuses   []int64  // 空 = 不限
		TaskType   string
		Priorities []int64
		After      *TaskCursor
		Limit      int
	}

	// TaskCursor 是上一页最后一行的排序键。
	TaskCursor struct {
		Priority int64
		DueAt    time.Time // 空截止时间按 noDueAt 参与排序
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
	query := fmt.Sprintf("select %s from %s where `id` = ? limit 1 for update", taskRows, m.table)
	var resp Task
	switch err := m.conn.QueryRowCtx(ctx, &resp, query, id); err {
	case nil:
		return &resp, nil
	case sqlx.ErrNotFound:
		return nil, ErrNotFound
	default:
		return nil, err
	}
}

func (m *customTaskModel) ApplySync(ctx context.Context, id uint64, status int64, assignee, group, node, resolution string, snoozeUntil sql.NullTime, seq int64) (bool, error) {
	query := fmt.Sprintf("update %s set `status` = ?, `assignee_id` = ?, `assignee_group` = ?, `current_node` = ?, `resolution` = ?, `snooze_until` = ?, `seq` = ?, `version` = `version` + 1 where `id` = ? and `seq` < ?", m.table)
	return affected(m.conn.ExecCtx(ctx, query, status, assignee, group, node, resolution, snoozeUntil, seq, id, seq))
}

func (m *customTaskModel) CloseUnstarted(ctx context.Context, id uint64, status int64, resolution string) error {
	query := fmt.Sprintf("update %s set `status` = ?, `resolution` = ?, `version` = `version` + 1 where `id` = ? and `run_id` = 0 and `status` < ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, status, resolution, id, status)
	return err
}

func (m *customTaskModel) SetRun(ctx context.Context, id, runID uint64) error {
	query := fmt.Sprintf("update %s set `run_id` = ? where `id` = ? and `run_id` = 0", m.table)
	_, err := m.conn.ExecCtx(ctx, query, runID, id)
	return err
}

// noDueAt 是空截止时间的排序值：没有截止时间的任务排在同优先级的最后。
var noDueAt = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// SortDueAt 返回截止时间的排序值，构造 TaskCursor 时使用。
func SortDueAt(t sql.NullTime) time.Time {
	if t.Valid {
		return t.Time
	}
	return noDueAt
}

func (m *customTaskModel) List(ctx context.Context, f TaskFilter) ([]*Task, error) {
	where, args := f.where()
	if where == nil {
		return nil, nil
	}
	const dueKey = "coalesce(`due_at`, ?)"
	if f.After != nil {
		where = append(where, "(`priority`, "+dueKey+", `id`) > (?, ?, ?)")
		args = append(args, noDueAt, f.After.Priority, f.After.DueAt, f.After.ID)
	}
	query := fmt.Sprintf("select %s from %s where %s order by `priority`, %s, `id` limit ?",
		taskRows, m.table, strings.Join(where, " and "), dueKey)
	args = append(args, noDueAt, f.Limit)
	var out []*Task
	err := m.conn.QueryRowsCtx(ctx, &out, query, args...)
	return out, err
}

func (m *customTaskModel) Count(ctx context.Context, f TaskFilter) ([]TaskCount, error) {
	where, args := f.where()
	if where == nil {
		return nil, nil
	}
	query := fmt.Sprintf("select `priority`, `status`, count(*) as `n` from %s where %s group by `priority`, `status` order by `priority`, `status`",
		m.table, strings.Join(where, " and "))
	var out []TaskCount
	err := m.conn.QueryRowsCtx(ctx, &out, query, args...)
	return out, err
}

// where 返回 List 与 Count 共用的条件；没有指定处理人或组时返回 nil（查询结果为空）。
func (f TaskFilter) where() ([]string, []any) {
	var who []string
	var args []any
	if f.AssigneeID != "" {
		who = append(who, "`assignee_id` = ?")
		args = append(args, f.AssigneeID)
	}
	if len(f.Groups) > 0 {
		who = append(who, "(`assignee_id` = '' and `assignee_group` in ("+placeholders(len(f.Groups))+"))")
		for _, g := range f.Groups {
			args = append(args, g)
		}
	}
	if len(who) == 0 {
		return nil, nil
	}
	where := []string{"(" + strings.Join(who, " or ") + ")"}
	if len(f.Statuses) > 0 {
		where = append(where, "`status` in ("+placeholders(len(f.Statuses))+")")
		for _, v := range f.Statuses {
			args = append(args, v)
		}
	}
	if f.TaskType != "" {
		where = append(where, "`task_type` = ?")
		args = append(args, f.TaskType)
	}
	if len(f.Priorities) > 0 {
		where = append(where, "`priority` in ("+placeholders(len(f.Priorities))+")")
		for _, v := range f.Priorities {
			args = append(args, v)
		}
	}
	return where, args
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (m *customTaskModel) SetAssignment(ctx context.Context, id uint64, status int64, assignee, group string) error {
	query := fmt.Sprintf("update %s set `status` = ?, `assignee_id` = ?, `assignee_group` = ?, `version` = `version` + 1 where `id` = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, status, assignee, group, id)
	return err
}

func (m *customTaskModel) SetPriority(ctx context.Context, id uint64, priority int64) error {
	query := fmt.Sprintf("update %s set `priority` = ?, `version` = `version` + 1 where `id` = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, priority, id)
	return err
}
