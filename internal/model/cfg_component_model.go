package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ CfgComponentModel = (*customCfgComponentModel)(nil)

type (
	// CfgComponentModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgComponentModel.
	CfgComponentModel interface {
		cfgComponentModel
		WithSession(session sqlx.Session) CfgComponentModel
	}

	customCfgComponentModel struct {
		*defaultCfgComponentModel
	}
)

// NewCfgComponentModel returns a model for the database table.
func NewCfgComponentModel(conn sqlx.SqlConn) CfgComponentModel {
	return &customCfgComponentModel{
		defaultCfgComponentModel: newCfgComponentModel(conn),
	}
}

func (m *customCfgComponentModel) WithSession(session sqlx.Session) CfgComponentModel {
	return NewCfgComponentModel(sqlx.NewSqlConnFromSession(session))
}
