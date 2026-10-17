package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ WfLoopCounterModel = (*customWfLoopCounterModel)(nil)

type (
	// WfLoopCounterModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfLoopCounterModel.
	WfLoopCounterModel interface {
		wfLoopCounterModel
		WithSession(session sqlx.Session) WfLoopCounterModel
	}

	customWfLoopCounterModel struct {
		*defaultWfLoopCounterModel
	}
)

// NewWfLoopCounterModel returns a model for the database table.
func NewWfLoopCounterModel(conn sqlx.SqlConn) WfLoopCounterModel {
	return &customWfLoopCounterModel{
		defaultWfLoopCounterModel: newWfLoopCounterModel(conn),
	}
}

func (m *customWfLoopCounterModel) WithSession(session sqlx.Session) WfLoopCounterModel {
	return NewWfLoopCounterModel(sqlx.NewSqlConnFromSession(session))
}
