package model

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ WfScheduleModel = (*customWfScheduleModel)(nil)

type (
	// WfScheduleModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfScheduleModel.
	WfScheduleModel interface {
		wfScheduleModel
		WithSession(session sqlx.Session) WfScheduleModel
		// FindDue 返回已启用且到期的定时来源。
		FindDue(ctx context.Context, now time.Time, limit int) ([]*WfSchedule, error)
		// Advance 用乐观锁推进 next_fire_at；返回 false 表示已被其他实例推进，本次不触发。
		Advance(ctx context.Context, id uint64, version int64, next time.Time) (bool, error)
		// SaveResult 记录一次扫描后的游标与错误。
		SaveResult(ctx context.Context, id uint64, cursor, lastError string) error
	}

	customWfScheduleModel struct {
		*defaultWfScheduleModel
	}
)

// NewWfScheduleModel returns a model for the database table.
func NewWfScheduleModel(conn sqlx.SqlConn) WfScheduleModel {
	return &customWfScheduleModel{
		defaultWfScheduleModel: newWfScheduleModel(conn),
	}
}

func (m *customWfScheduleModel) WithSession(session sqlx.Session) WfScheduleModel {
	return NewWfScheduleModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customWfScheduleModel) FindDue(ctx context.Context, now time.Time, limit int) ([]*WfSchedule, error) {
	query := fmt.Sprintf("select %s from %s where `enabled` = 1 and `next_fire_at` <= ? order by `next_fire_at` limit ?", wfScheduleRows, m.table)
	var resp []*WfSchedule
	err := m.conn.QueryRowsCtx(ctx, &resp, query, now, limit)
	return resp, err
}

func (m *customWfScheduleModel) Advance(ctx context.Context, id uint64, version int64, next time.Time) (bool, error) {
	query := fmt.Sprintf("update %s set `next_fire_at` = ?, `version` = `version` + 1 where `id` = ? and `version` = ?", m.table)
	return affected(m.conn.ExecCtx(ctx, query, next, id, version))
}

func (m *customWfScheduleModel) SaveResult(ctx context.Context, id uint64, cursor, lastError string) error {
	query := fmt.Sprintf("update %s set `scan_cursor` = ?, `last_error` = ? where `id` = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, cursor, truncate(lastError, 1024), id)
	return err
}
