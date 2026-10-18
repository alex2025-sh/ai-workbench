package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ CfgDraftModel = (*customCfgDraftModel)(nil)

type (
	// CfgDraftModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCfgDraftModel.
	CfgDraftModel interface {
		cfgDraftModel
		WithSession(session sqlx.Session) CfgDraftModel
		// SaveIfRevision 在 revision 等于 expected 时写入新内容并把 revision 加 1，返回是否写入（乐观锁）。
		SaveIfRevision(ctx context.Context, id uint64, expected int64, body, updatedBy string) (bool, error)
		// DeleteIfRevision 在 revision 等于 expected 时删除草稿，返回是否删除。
		DeleteIfRevision(ctx context.Context, id uint64, expected int64) (bool, error)
		// ComponentIDs 返回有草稿的配置对象 id。
		ComponentIDs(ctx context.Context) ([]uint64, error)
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

func (m *customCfgDraftModel) SaveIfRevision(ctx context.Context, id uint64, expected int64, body, updatedBy string) (bool, error) {
	query := fmt.Sprintf("update %s set `body` = ?, `revision` = `revision` + 1, `updated_by` = ? where `id` = ? and `revision` = ?", m.table)
	return affected(m.conn.ExecCtx(ctx, query, body, updatedBy, id, expected))
}

func (m *customCfgDraftModel) DeleteIfRevision(ctx context.Context, id uint64, expected int64) (bool, error) {
	query := fmt.Sprintf("delete from %s where `id` = ? and `revision` = ?", m.table)
	return affected(m.conn.ExecCtx(ctx, query, id, expected))
}

func (m *customCfgDraftModel) ComponentIDs(ctx context.Context) ([]uint64, error) {
	var ids []uint64
	err := m.conn.QueryRowsCtx(ctx, &ids, fmt.Sprintf("select `component_id` from %s", m.table))
	return ids, err
}
