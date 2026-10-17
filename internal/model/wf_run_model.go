package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ WfRunModel = (*customWfRunModel)(nil)

type (
	// WfRunModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfRunModel.
	WfRunModel interface {
		wfRunModel
		WithSession(session sqlx.Session) WfRunModel
		// FindOneForUpdate 在事务内锁住 run 行，串行化同一 run 的推进。
		FindOneForUpdate(ctx context.Context, id uint64) (*WfRun, error)
		// UpdateState 更新状态、版本与执行计数，变量不变。
		UpdateState(ctx context.Context, id uint64, status string, version, execCount int64) error
	}

	customWfRunModel struct {
		*defaultWfRunModel
	}
)

// NewWfRunModel returns a model for the database table.
func NewWfRunModel(conn sqlx.SqlConn) WfRunModel {
	return &customWfRunModel{
		defaultWfRunModel: newWfRunModel(conn),
	}
}

func (m *customWfRunModel) WithSession(session sqlx.Session) WfRunModel {
	return NewWfRunModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customWfRunModel) FindOneForUpdate(ctx context.Context, id uint64) (*WfRun, error) {
	query := fmt.Sprintf("select %s from %s where `id` = ? limit 1 for update", wfRunRows, m.table)
	var resp WfRun
	switch err := m.conn.QueryRowCtx(ctx, &resp, query, id); err {
	case nil:
		return &resp, nil
	case sqlx.ErrNotFound:
		return nil, ErrNotFound
	default:
		return nil, err
	}
}

func (m *customWfRunModel) UpdateState(ctx context.Context, id uint64, status string, version, execCount int64) error {
	query := fmt.Sprintf("update %s set `status` = ?, `version` = ?, `exec_count` = ? where `id` = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, status, version, execCount, id)
	return err
}
