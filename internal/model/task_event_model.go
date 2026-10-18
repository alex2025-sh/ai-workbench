package model

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskEventModel = (*customTaskEventModel)(nil)

type (
	// TaskEventModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskEventModel.
	TaskEventModel interface {
		taskEventModel
		WithSession(session sqlx.Session) TaskEventModel
		// ListByTask 按 (created_at, id) 顺序分页返回任务事件；after 为上一页最后一条，nil 表示从头开始。
		ListByTask(ctx context.Context, taskID uint64, after *TaskEventCursor, limit int) ([]*TaskEvent, error)
	}

	// TaskEventCursor 是上一页最后一条事件的排序键。自增 id 不保证单调，按写入时间排序，id 只用于打破并列。
	TaskEventCursor struct {
		CreatedAt time.Time
		ID        uint64
	}

	customTaskEventModel struct {
		*defaultTaskEventModel
	}
)

// NewTaskEventModel returns a model for the database table.
func NewTaskEventModel(conn sqlx.SqlConn) TaskEventModel {
	return &customTaskEventModel{
		defaultTaskEventModel: newTaskEventModel(conn),
	}
}

func (m *customTaskEventModel) WithSession(session sqlx.Session) TaskEventModel {
	return NewTaskEventModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTaskEventModel) ListByTask(ctx context.Context, taskID uint64, after *TaskEventCursor, limit int) ([]*TaskEvent, error) {
	query := fmt.Sprintf("select %s from %s where `task_id` = ?", taskEventRows, m.table)
	args := []any{taskID}
	if after != nil {
		query += " and (`created_at`, `id`) > (?, ?)"
		args = append(args, after.CreatedAt, after.ID)
	}
	query += " order by `created_at`, `id` limit ?"
	args = append(args, limit)
	var out []*TaskEvent
	err := m.conn.QueryRowsCtx(ctx, &out, query, args...)
	return out, err
}
