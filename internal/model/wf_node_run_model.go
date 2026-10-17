package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ WfNodeRunModel = (*customWfNodeRunModel)(nil)

type (
	// WfNodeRunModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfNodeRunModel.
	WfNodeRunModel interface {
		wfNodeRunModel
		WithSession(session sqlx.Session) WfNodeRunModel
		// FindByRun 返回一个 run 的全部节点，按 id 排序。
		FindByRun(ctx context.Context, runID uint64) ([]*WfNodeRun, error)
	}

	customWfNodeRunModel struct {
		*defaultWfNodeRunModel
	}
)

// NewWfNodeRunModel returns a model for the database table.
func NewWfNodeRunModel(conn sqlx.SqlConn) WfNodeRunModel {
	return &customWfNodeRunModel{
		defaultWfNodeRunModel: newWfNodeRunModel(conn),
	}
}

func (m *customWfNodeRunModel) WithSession(session sqlx.Session) WfNodeRunModel {
	return NewWfNodeRunModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customWfNodeRunModel) FindByRun(ctx context.Context, runID uint64) ([]*WfNodeRun, error) {
	query := fmt.Sprintf("select %s from %s where `run_id` = ? order by `id`", wfNodeRunRows, m.table)
	var resp []*WfNodeRun
	err := m.conn.QueryRowsCtx(ctx, &resp, query, runID)
	return resp, err
}
