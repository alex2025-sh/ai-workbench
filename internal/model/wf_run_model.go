package model

import (
	"context"
	"fmt"
	"strings"
	"time"

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
		// List 按创建时间倒序分页查询正式 run（不含试运行），键集分页。
		List(ctx context.Context, f RunFilter) ([]*WfRun, error)
	}

	// RunFilter 是 run 列表的查询条件，空值表示不限。
	RunFilter struct {
		IDs           []uint64 // 按任务、biz_key 查询时先解析出的 run
		Status        string
		PlanID        string
		CreatedAfter  time.Time
		CreatedBefore time.Time
		Before        *RunCursor
		Limit         int
	}

	// RunCursor 是上一页最后一行的排序键。
	RunCursor struct {
		CreatedAt time.Time
		ID        uint64
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

func (m *customWfRunModel) List(ctx context.Context, f RunFilter) ([]*WfRun, error) {
	where := []string{"`dry_run` = 0"}
	var args []any
	if len(f.IDs) > 0 {
		where = append(where, "`id` in ("+placeholders(len(f.IDs))+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.Status != "" {
		where = append(where, "`status` = ?")
		args = append(args, f.Status)
	}
	if f.PlanID != "" {
		where = append(where, "`plan_id` = ?")
		args = append(args, f.PlanID)
	}
	if !f.CreatedAfter.IsZero() {
		where = append(where, "`created_at` >= ?")
		args = append(args, f.CreatedAfter)
	}
	if !f.CreatedBefore.IsZero() {
		where = append(where, "`created_at` < ?")
		args = append(args, f.CreatedBefore)
	}
	if f.Before != nil {
		where = append(where, "(`created_at`, `id`) < (?, ?)")
		args = append(args, f.Before.CreatedAt, f.Before.ID)
	}
	query := fmt.Sprintf("select %s from %s where %s order by `created_at` desc, `id` desc limit ?", wfRunRows, m.table, strings.Join(where, " and "))
	args = append(args, f.Limit)
	var out []*WfRun
	err := m.conn.QueryRowsCtx(ctx, &out, query, args...)
	return out, err
}
