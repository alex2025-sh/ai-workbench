package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TaskEventModel = (*customTaskEventModel)(nil)

type (
	// TaskEventModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskEventModel.
	TaskEventModel interface {
		taskEventModel
		WithSession(session sqlx.Session) TaskEventModel
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
