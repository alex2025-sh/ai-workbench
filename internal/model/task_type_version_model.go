package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskTypeVersionModel = (*customTaskTypeVersionModel)(nil)

type (
	// TaskTypeVersionModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskTypeVersionModel.
	TaskTypeVersionModel interface {
		taskTypeVersionModel
		WithSession(session sqlx.Session) TaskTypeVersionModel
		// ListByKey 返回任务类型的全部版本，新版本在前。
		ListByKey(ctx context.Context, key string) ([]*TaskTypeVersion, error)
	}

	customTaskTypeVersionModel struct {
		*defaultTaskTypeVersionModel
	}
)

// NewTaskTypeVersionModel returns a model for the database table.
func NewTaskTypeVersionModel(conn sqlx.SqlConn) TaskTypeVersionModel {
	return &customTaskTypeVersionModel{
		defaultTaskTypeVersionModel: newTaskTypeVersionModel(conn),
	}
}

func (m *customTaskTypeVersionModel) WithSession(session sqlx.Session) TaskTypeVersionModel {
	return NewTaskTypeVersionModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTaskTypeVersionModel) ListByKey(ctx context.Context, key string) ([]*TaskTypeVersion, error) {
	var out []*TaskTypeVersion
	q := fmt.Sprintf("select %s from %s where `type_key` = ? order by `version` desc", taskTypeVersionRows, m.table)
	return out, m.conn.QueryRowsCtx(ctx, &out, q, key)
}
