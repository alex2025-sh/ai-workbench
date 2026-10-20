package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskEventModel = (*customTaskEventModel)(nil)

type (
	// TaskEventModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskEventModel.
	TaskEventModel interface {
		taskEventModel
		WithSession(session sqlx.Session) TaskEventModel
		// ListByTask 返回任务的全部历史，按发生顺序。
		ListByTask(ctx context.Context, taskID uint64) ([]*TaskEvent, error)
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

func (m *customTaskEventModel) ListByTask(ctx context.Context, taskID uint64) ([]*TaskEvent, error) {
	var out []*TaskEvent
	return out, m.conn.QueryRowsCtx(ctx, &out, fmt.Sprintf("select %s from %s where `task_id` = ? order by `id`", taskEventRows, m.table), taskID)
}
