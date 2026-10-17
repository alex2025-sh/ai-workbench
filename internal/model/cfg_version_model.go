package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ CfgVersionModel = (*customCfgVersionModel)(nil)

type (
	// CfgVersionModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgVersionModel.
	CfgVersionModel interface {
		cfgVersionModel
		WithSession(session sqlx.Session) CfgVersionModel
	}

	customCfgVersionModel struct {
		*defaultCfgVersionModel
	}
)

// NewCfgVersionModel returns a model for the database table.
func NewCfgVersionModel(conn sqlx.SqlConn) CfgVersionModel {
	return &customCfgVersionModel{
		defaultCfgVersionModel: newCfgVersionModel(conn),
	}
}

func (m *customCfgVersionModel) WithSession(session sqlx.Session) CfgVersionModel {
	return NewCfgVersionModel(sqlx.NewSqlConnFromSession(session))
}
