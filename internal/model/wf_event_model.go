package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ WfEventModel = (*customWfEventModel)(nil)

type (
	// WfEventModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfEventModel.
	WfEventModel interface {
		wfEventModel
		WithSession(session sqlx.Session) WfEventModel
	}

	customWfEventModel struct {
		*defaultWfEventModel
	}
)

// NewWfEventModel returns a model for the database table.
func NewWfEventModel(conn sqlx.SqlConn) WfEventModel {
	return &customWfEventModel{
		defaultWfEventModel: newWfEventModel(conn),
	}
}

func (m *customWfEventModel) WithSession(session sqlx.Session) WfEventModel {
	return NewWfEventModel(sqlx.NewSqlConnFromSession(session))
}
