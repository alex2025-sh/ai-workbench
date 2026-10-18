package model

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ WfEventModel = (*customWfEventModel)(nil)

type (
	// WfEventModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfEventModel.
	WfEventModel interface {
		wfEventModel
		WithSession(session sqlx.Session) WfEventModel
		// ListByRun 按 (created_at, id) 顺序分页返回 run 的事件；after 为上一页最后一条，nil 表示从头开始。
		ListByRun(ctx context.Context, runID uint64, after *EventCursor, limit int) ([]*WfEvent, error)
	}

	// EventCursor 是上一页最后一条事件的排序键。自增 id 不保证单调，按写入时间排序，id 只用于打破并列。
	EventCursor struct {
		CreatedAt time.Time
		ID        uint64
	}

	customWfEventModel struct {
		*defaultWfEventModel
	}
)

// NewWfEventModel returns a model for the database table.
func NewWfEventModel(conn sqlx.SqlConn) WfEventModel {
	return &customWfEventModel{
		defaultWfEventModel: newWfEventModel(conn),
	}
}

func (m *customWfEventModel) WithSession(session sqlx.Session) WfEventModel {
	return NewWfEventModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customWfEventModel) ListByRun(ctx context.Context, runID uint64, after *EventCursor, limit int) ([]*WfEvent, error) {
	query := fmt.Sprintf("select %s from %s where `run_id` = ?", wfEventRows, m.table)
	args := []any{runID}
	if after != nil {
		query += " and (`created_at`, `id`) > (?, ?)"
		args = append(args, after.CreatedAt, after.ID)
	}
	query += " order by `created_at`, `id` limit ?"
	args = append(args, limit)
	var out []*WfEvent
	err := m.conn.QueryRowsCtx(ctx, &out, query, args...)
	return out, err
}
