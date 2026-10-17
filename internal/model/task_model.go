package model

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskModel = (*customTaskModel)(nil)

type (
	// TaskModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskModel.
	TaskModel interface {
		taskModel
		WithSession(session sqlx.Session) TaskModel
		// FindOneForUpdate 在事务内锁住任务行。
		FindOneForUpdate(ctx context.Context, id uint64) (*Task, error)
		// ApplySync 应用引擎同步：只有 seq 更大时才写入，返回是否生效。
		ApplySync(ctx context.Context, id uint64, status int64, assignee, group, node, resolution string, snoozeUntil sql.NullTime, seq int64) (bool, error)
		// SetRun 记录驱动任务的 run（只在尚未设置时写入）。
		SetRun(ctx context.Context, id, runID uint64) error
	}

	customTaskModel struct {
		*defaultTaskModel
	}
)

// NewTaskModel returns a model for the database table.
func NewTaskModel(conn sqlx.SqlConn) TaskModel {
	return &customTaskModel{
		defaultTaskModel: newTaskModel(conn),
	}
}

func (m *customTaskModel) WithSession(session sqlx.Session) TaskModel {
	return NewTaskModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTaskModel) FindOneForUpdate(ctx context.Context, id uint64) (*Task, error) {
	query := fmt.Sprintf("select %s from %s where `id` = ? limit 1 for update", taskRows, m.table)
	var resp Task
	switch err := m.conn.QueryRowCtx(ctx, &resp, query, id); err {
	case nil:
		return &resp, nil
	case sqlx.ErrNotFound:
		return nil, ErrNotFound
	default:
		return nil, err
	}
}

func (m *customTaskModel) ApplySync(ctx context.Context, id uint64, status int64, assignee, group, node, resolution string, snoozeUntil sql.NullTime, seq int64) (bool, error) {
	query := fmt.Sprintf("update %s set `status` = ?, `assignee_id` = ?, `assignee_group` = ?, `current_node` = ?, `resolution` = ?, `snooze_until` = ?, `seq` = ?, `version` = `version` + 1 where `id` = ? and `seq` < ?", m.table)
	return affected(m.conn.ExecCtx(ctx, query, status, assignee, group, node, resolution, snoozeUntil, seq, id, seq))
}

func (m *customTaskModel) SetRun(ctx context.Context, id, runID uint64) error {
	query := fmt.Sprintf("update %s set `run_id` = ? where `id` = ? and `run_id` = 0", m.table)
	_, err := m.conn.ExecCtx(ctx, query, runID, id)
	return err
}
