package model

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ SourceStateModel = (*customSourceStateModel)(nil)

type (
	// SourceStateModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSourceStateModel.
	SourceStateModel interface {
		sourceStateModel
		// Ensure 在没有记录时插入一行（下一次执行时间为 at），已有记录不变。
		Ensure(ctx context.Context, taskType string, at time.Time) error
		// Lease 抢占一次到期的执行：next_run_at 已到且没有未过期的租约时，写入租约与下一次执行时间，返回是否抢到。
		Lease(ctx context.Context, taskType string, now, leaseUntil, next time.Time) (bool, error)
		// Finish 记录一次执行的结果并释放租约。
		Finish(ctx context.Context, taskType string, at time.Time, rows, created, closed int, lastErr string) error
		// List 返回全部来源状态。
		List(ctx context.Context) ([]*SourceState, error)
	}

	customSourceStateModel struct {
		*defaultSourceStateModel
	}
)

// NewSourceStateModel returns a model for the database table.
func NewSourceStateModel(conn sqlx.SqlConn) SourceStateModel {
	return &customSourceStateModel{
		defaultSourceStateModel: newSourceStateModel(conn),
	}
}

func (m *customSourceStateModel) Ensure(ctx context.Context, taskType string, at time.Time) error {
	_, err := m.conn.ExecCtx(ctx, fmt.Sprintf("insert ignore into %s (`task_type`, `next_run_at`) values (?, ?)", m.table), taskType, at)
	return err
}

func (m *customSourceStateModel) Lease(ctx context.Context, taskType string, now, leaseUntil, next time.Time) (bool, error) {
	q := fmt.Sprintf("update %s set `lease_until` = ?, `next_run_at` = ? where `task_type` = ? and `next_run_at` <= ? and (`lease_until` is null or `lease_until` < ?)", m.table)
	return affected(m.conn.ExecCtx(ctx, q, leaseUntil, next, taskType, now, now))
}

func (m *customSourceStateModel) Finish(ctx context.Context, taskType string, at time.Time, rows, created, closed int, lastErr string) error {
	if len(lastErr) > 500 {
		lastErr = lastErr[:500]
	}
	q := fmt.Sprintf("update %s set `lease_until` = null, `last_run_at` = ?, `last_rows` = ?, `last_created` = ?, `last_closed` = ?, `last_error` = ? where `task_type` = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, q, at, rows, created, closed, lastErr, taskType)
	return err
}

func (m *customSourceStateModel) List(ctx context.Context) ([]*SourceState, error) {
	var out []*SourceState
	return out, m.conn.QueryRowsCtx(ctx, &out, fmt.Sprintf("select %s from %s order by `task_type`", sourceStateRows, m.table))
}
