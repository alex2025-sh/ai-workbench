package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ CfgDraftModel = (*customCfgDraftModel)(nil)

type (
	// CfgDraftModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgDraftModel.
	CfgDraftModel interface {
		cfgDraftModel
		WithSession(session sqlx.Session) CfgDraftModel
	}

	customCfgDraftModel struct {
		*defaultCfgDraftModel
	}
)

// NewCfgDraftModel returns a model for the database table.
func NewCfgDraftModel(conn sqlx.SqlConn) CfgDraftModel {
	return &customCfgDraftModel{
		defaultCfgDraftModel: newCfgDraftModel(conn),
	}
}

func (m *customCfgDraftModel) WithSession(session sqlx.Session) CfgDraftModel {
	return NewCfgDraftModel(sqlx.NewSqlConnFromSession(session))
}
