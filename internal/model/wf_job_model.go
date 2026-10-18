package model

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ WfJobModel = (*customWfJobModel)(nil)

type (
	// WfJobModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfJobModel.
	//
	// job 的状态只在锁住所属 run 行的事务里修改（读出整行、改字段、按主键 Update），
	// 因此这里不需要 CAS 类的条件更新。
	WfJobModel interface {
		wfJobModel
		WithSession(session sqlx.Session) WfJobModel
		// InsertIgnore 按 idem_key 去重插入；已存在时返回 false，不报错。
		InsertIgnore(ctx context.Context, data *WfJob) (bool, error)
		// FindDue 返回到期、未被认领（或租约已过期）的 job，只是候选：是否真能处理要在 run 锁内再判断。
		FindDue(ctx context.Context, now time.Time, limit int) ([]*WfJob, error)
		// FindPendingByRun 返回一个 run 中尚未结束的 job，按 id 排序。
		FindPendingByRun(ctx context.Context, runID uint64) ([]*WfJob, error)
	}

	customWfJobModel struct {
		*defaultWfJobModel
	}
)

// NewWfJobModel returns a model for the database table.
func NewWfJobModel(conn sqlx.SqlConn) WfJobModel {
	return &customWfJobModel{
		defaultWfJobModel: newWfJobModel(conn),
	}
}

func (m *customWfJobModel) WithSession(session sqlx.Session) WfJobModel {
	return NewWfJobModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customWfJobModel) InsertIgnore(ctx context.Context, data *WfJob) (bool, error) {
	query := fmt.Sprintf("insert ignore into %s (%s) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", m.table, wfJobRowsExpectAutoSet)
	ret, err := m.conn.ExecCtx(ctx, query, data.RunId, data.NodeId, data.Iteration, data.Kind, data.Payload, data.DueAt, data.Status, data.Attempt, data.FailCount, data.Owner, data.LockedUntil, data.LastError, data.IdemKey)
	if err != nil {
		return false, err
	}
	n, err := ret.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		id, err := ret.LastInsertId()
		if err != nil {
			return false, err
		}
		data.Id = uint64(id)
	}
	return n == 1, nil
}

func (m *customWfJobModel) FindDue(ctx context.Context, now time.Time, limit int) ([]*WfJob, error) {
	query := fmt.Sprintf("select %s from %s where `status` = 'pending' and `due_at` <= ? and (`owner` = '' or `locked_until` < ?) order by `due_at` limit ?", wfJobRows, m.table)
	var resp []*WfJob
	err := m.conn.QueryRowsCtx(ctx, &resp, query, now, now, limit)
	return resp, err
}

func (m *customWfJobModel) FindPendingByRun(ctx context.Context, runID uint64) ([]*WfJob, error) {
	query := fmt.Sprintf("select %s from %s where `run_id` = ? and `status` = 'pending' order by `id`", wfJobRows, m.table)
	var resp []*WfJob
	err := m.conn.QueryRowsCtx(ctx, &resp, query, runID)
	return resp, err
}
