package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ WfNodeRunHistoryModel = (*customWfNodeRunHistoryModel)(nil)

type (
	// WfNodeRunHistoryModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfNodeRunHistoryModel.
	WfNodeRunHistoryModel interface {
		wfNodeRunHistoryModel
		WithSession(session sqlx.Session) WfNodeRunHistoryModel
	}

	customWfNodeRunHistoryModel struct {
		*defaultWfNodeRunHistoryModel
	}
)

// NewWfNodeRunHistoryModel returns a model for the database table.
func NewWfNodeRunHistoryModel(conn sqlx.SqlConn) WfNodeRunHistoryModel {
	return &customWfNodeRunHistoryModel{
		defaultWfNodeRunHistoryModel: newWfNodeRunHistoryModel(conn),
	}
}

func (m *customWfNodeRunHistoryModel) WithSession(session sqlx.Session) WfNodeRunHistoryModel {
	return NewWfNodeRunHistoryModel(sqlx.NewSqlConnFromSession(session))
}
