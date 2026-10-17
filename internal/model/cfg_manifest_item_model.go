package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ CfgManifestItemModel = (*customCfgManifestItemModel)(nil)

type (
	// CfgManifestItemModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgManifestItemModel.
	CfgManifestItemModel interface {
		cfgManifestItemModel
		WithSession(session sqlx.Session) CfgManifestItemModel
	}

	customCfgManifestItemModel struct {
		*defaultCfgManifestItemModel
	}
)

// NewCfgManifestItemModel returns a model for the database table.
func NewCfgManifestItemModel(conn sqlx.SqlConn) CfgManifestItemModel {
	return &customCfgManifestItemModel{
		defaultCfgManifestItemModel: newCfgManifestItemModel(conn),
	}
}

func (m *customCfgManifestItemModel) WithSession(session sqlx.Session) CfgManifestItemModel {
	return NewCfgManifestItemModel(sqlx.NewSqlConnFromSession(session))
}
