package model

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ OutboxModel = (*customOutboxModel)(nil)

// outbox 的状态。
const (
	OutboxPending = 0
	OutboxDone    = 1
	OutboxDead    = 2
)

type (
	// OutboxModel is an interface to be customized, add more methods here,
	// and implement the added methods in customOutboxModel.
	OutboxModel interface {
		outboxModel
		WithSession(session sqlx.Session) OutboxModel
		// Add 写入一条待投递记录，幂等键已存在时忽略。
		Add(ctx context.Context, o *Outbox) error
		// Due 返回到期待投递的记录 id。
		Due(ctx context.Context, now time.Time, limit int) ([]uint64, error)
		// ListByTask 返回任务的全部投递记录（只读，排查用）。
		ListByTask(ctx context.Context, taskID uint64) ([]*Outbox, error)
		// Claim 把一条待投递记录的下次投递时间推到 until，返回是否抢到（多实例只有一个投递）。
		Claim(ctx context.Context, id uint64, now, until time.Time) (bool, error)
		// Done 标记投递成功。
		Done(ctx context.Context, id uint64) error
		// Failed 记录一次失败：dead 为 true 时放弃，否则在 next 重试。
		Failed(ctx context.Context, id uint64, errText string, next time.Time, dead bool) error
	}

	customOutboxModel struct {
		*defaultOutboxModel
	}
)

// NewOutboxModel returns a model for the database table.
func NewOutboxModel(conn sqlx.SqlConn) OutboxModel {
	return &customOutboxModel{
		defaultOutboxModel: newOutboxModel(conn),
	}
}

func (m *customOutboxModel) WithSession(session sqlx.Session) OutboxModel {
	return NewOutboxModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customOutboxModel) Add(ctx context.Context, o *Outbox) error {
	q := fmt.Sprintf("insert ignore into %s (`task_id`, `kind`, `target`, `payload`, `idem_key`, `next_at`) values (?, ?, ?, ?, ?, ?)", m.table)
	_, err := m.conn.ExecCtx(ctx, q, o.TaskId, o.Kind, o.Target, o.Payload, o.IdemKey, o.NextAt)
	return err
}

func (m *customOutboxModel) Due(ctx context.Context, now time.Time, limit int) ([]uint64, error) {
	var out []uint64
	q := fmt.Sprintf("select `id` from %s where `status` = 0 and `next_at` <= ? order by `next_at` limit ?", m.table)
	return out, m.conn.QueryRowsCtx(ctx, &out, q, now, limit)
}

func (m *customOutboxModel) ListByTask(ctx context.Context, taskID uint64) ([]*Outbox, error) {
	var out []*Outbox
	return out, m.conn.QueryRowsCtx(ctx, &out, fmt.Sprintf("select %s from %s where `task_id` = ? order by `id`", outboxRows, m.table), taskID)
}

func (m *customOutboxModel) Claim(ctx context.Context, id uint64, now, until time.Time) (bool, error) {
	q := fmt.Sprintf("update %s set `next_at` = ?, `attempts` = `attempts` + 1 where `id` = ? and `status` = 0 and `next_at` <= ?", m.table)
	return affected(m.conn.ExecCtx(ctx, q, until, id, now))
}

func (m *customOutboxModel) Done(ctx context.Context, id uint64) error {
	_, err := m.conn.ExecCtx(ctx, fmt.Sprintf("update %s set `status` = 1, `last_error` = '' where `id` = ?", m.table), id)
	return err
}

func (m *customOutboxModel) Failed(ctx context.Context, id uint64, errText string, next time.Time, dead bool) error {
	if len(errText) > 500 {
		errText = errText[:500]
	}
	status := OutboxPending
	if dead {
		status = OutboxDead
	}
	_, err := m.conn.ExecCtx(ctx, fmt.Sprintf("update %s set `status` = ?, `last_error` = ?, `next_at` = ? where `id` = ?", m.table), status, errText, next, id)
	return err
}
