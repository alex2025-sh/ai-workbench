package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ CfgChangeSetItemModel = (*customCfgChangeSetItemModel)(nil)

type (
	// CfgChangeSetItemModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgChangeSetItemModel.
	CfgChangeSetItemModel interface {
		cfgChangeSetItemModel
		WithSession(session sqlx.Session) CfgChangeSetItemModel
	}

	customCfgChangeSetItemModel struct {
		*defaultCfgChangeSetItemModel
	}
)

// NewCfgChangeSetItemModel returns a model for the database table.
func NewCfgChangeSetItemModel(conn sqlx.SqlConn) CfgChangeSetItemModel {
	return &customCfgChangeSetItemModel{
		defaultCfgChangeSetItemModel: newCfgChangeSetItemModel(conn),
	}
}

func (m *customCfgChangeSetItemModel) WithSession(session sqlx.Session) CfgChangeSetItemModel {
	return NewCfgChangeSetItemModel(sqlx.NewSqlConnFromSession(session))
}
