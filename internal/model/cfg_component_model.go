package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ CfgComponentModel = (*customCfgComponentModel)(nil)

type (
	// CfgComponentModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgComponentModel.
	CfgComponentModel interface {
		cfgComponentModel
		WithSession(session sqlx.Session) CfgComponentModel
		// ListByKind 返回某类配置对象，按 key 排序。
		ListByKind(ctx context.Context, kind string) ([]*CfgComponent, error)
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

func (m *customCfgComponentModel) ListByKind(ctx context.Context, kind string) ([]*CfgComponent, error) {
	var out []*CfgComponent
	err := m.conn.QueryRowsCtx(ctx, &out, fmt.Sprintf("select %s from %s where `kind` = ? order by `comp_key`", cfgComponentRows, m.table), kind)
	return out, err
}
