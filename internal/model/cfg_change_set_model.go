package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ CfgChangeSetModel = (*customCfgChangeSetModel)(nil)

type (
	// CfgChangeSetModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgChangeSetModel.
	CfgChangeSetModel interface {
		cfgChangeSetModel
		WithSession(session sqlx.Session) CfgChangeSetModel
	}

	customCfgChangeSetModel struct {
		*defaultCfgChangeSetModel
	}
)

// NewCfgChangeSetModel returns a model for the database table.
func NewCfgChangeSetModel(conn sqlx.SqlConn) CfgChangeSetModel {
	return &customCfgChangeSetModel{
		defaultCfgChangeSetModel: newCfgChangeSetModel(conn),
	}
}

func (m *customCfgChangeSetModel) WithSession(session sqlx.Session) CfgChangeSetModel {
	return NewCfgChangeSetModel(sqlx.NewSqlConnFromSession(session))
}
