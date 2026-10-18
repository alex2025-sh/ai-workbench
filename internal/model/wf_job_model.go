package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ WfJobModel = (*customWfJobModel)(nil)

type (
	// WfJobModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfJobModel.
	WfJobModel interface {
		wfJobModel
		WithSession(session sqlx.Session) WfJobModel
		// InsertIgnore 按 idem_key 去重插入；已存在时返回 false，不报错。
		InsertIgnore(ctx context.Context, data *WfJob) (bool, error)
		// FindDue 返回到期、未被认领（或租约已过期）的 job。
		FindDue(ctx context.Context, now time.Time, limit int) ([]*WfJob, error)
		// Claim 用 CAS 认领 job，attempt 加 1；返回 false 表示被别人抢走。
		// 认领成功后 (owner, attempt) 就是这次认领的凭证，之后对 job 的所有修改都必须带上它。
		Claim(ctx context.Context, id uint64, owner string, now, lockedUntil time.Time) (bool, error)
		// LockHeld 在事务内锁住 job 行，并报告它是否仍由凭证 (owner, attempt) 持有且未结束。
		LockHeld(ctx context.Context, id uint64, owner string, attempt int64) (bool, error)
		// Extend 由持有者延长租约（只延长，不缩短）。
		Extend(ctx context.Context, id uint64, owner string, attempt int64, lockedUntil time.Time) (bool, error)
		// Finish 由持有者把 job 置为终态（done / cancelled / failed）。
		Finish(ctx context.Context, id uint64, owner string, attempt int64, status, lastError string) (bool, error)
		// Release 由持有者释放租约并重新排期，用于节点重试。
		Release(ctx context.Context, id uint64, owner string, attempt int64, dueAt time.Time, lastError string) (bool, error)
		// Backoff 由持有者在处理失败（加载或提交出错）后释放租约、重新排期，并把 fail_count 加 1。
		Backoff(ctx context.Context, id uint64, owner string, attempt int64, dueAt time.Time, lastError string) (bool, error)
		// CancelPending 取消某节点某一轮中尚未执行的指定类型 job。
		CancelPending(ctx context.Context, runID uint64, nodeID string, iteration int64, kinds []string) error
		// CancelRun 取消一个 run 中全部尚未执行的 job（sync_task 除外）。
		CancelRun(ctx context.Context, runID uint64) error
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

func (m *customWfJobModel) Claim(ctx context.Context, id uint64, owner string, now, lockedUntil time.Time) (bool, error) {
	query := fmt.Sprintf("update %s set `owner` = ?, `locked_until` = ?, `attempt` = `attempt` + 1 where `id` = ? and `status` = 'pending' and `due_at` <= ? and (`owner` = '' or `locked_until` < ?)", m.table)
	return affected(m.conn.ExecCtx(ctx, query, owner, lockedUntil, id, now, now))
}

// held 是“仍由这次认领持有”的条件：同一进程的多次认领 owner 相同，只有 attempt 能区分新旧。
const held = "`id` = ? and `owner` = ? and `attempt` = ? and `status` = 'pending'"

func (m *customWfJobModel) LockHeld(ctx context.Context, id uint64, owner string, attempt int64) (bool, error) {
	query := fmt.Sprintf("select `id` from %s where "+held+" for update", m.table)
	var got uint64
	switch err := m.conn.QueryRowCtx(ctx, &got, query, id, owner, attempt); err {
	case nil:
		return true, nil
	case sqlx.ErrNotFound:
		return false, nil
	default:
		return false, err
	}
}

func (m *customWfJobModel) Extend(ctx context.Context, id uint64, owner string, attempt int64, lockedUntil time.Time) (bool, error) {
	query := fmt.Sprintf("update %s set `locked_until` = greatest(coalesce(`locked_until`, ?), ?) where "+held, m.table)
	return affected(m.conn.ExecCtx(ctx, query, lockedUntil, lockedUntil, id, owner, attempt))
}

func (m *customWfJobModel) Finish(ctx context.Context, id uint64, owner string, attempt int64, status, lastError string) (bool, error) {
	query := fmt.Sprintf("update %s set `status` = ?, `last_error` = ?, `locked_until` = null where "+held, m.table)
	return affected(m.conn.ExecCtx(ctx, query, status, truncate(lastError, 1024), id, owner, attempt))
}

func (m *customWfJobModel) Release(ctx context.Context, id uint64, owner string, attempt int64, dueAt time.Time, lastError string) (bool, error) {
	query := fmt.Sprintf("update %s set `owner` = '', `locked_until` = null, `due_at` = ?, `last_error` = ? where "+held, m.table)
	return affected(m.conn.ExecCtx(ctx, query, dueAt, truncate(lastError, 1024), id, owner, attempt))
}

func (m *customWfJobModel) Backoff(ctx context.Context, id uint64, owner string, attempt int64, dueAt time.Time, lastError string) (bool, error) {
	query := fmt.Sprintf("update %s set `owner` = '', `locked_until` = null, `due_at` = ?, `last_error` = ?, `fail_count` = `fail_count` + 1 where "+held, m.table)
	return affected(m.conn.ExecCtx(ctx, query, dueAt, truncate(lastError, 1024), id, owner, attempt))
}

// CancelPending 与 CancelRun 先用普通读取找出 id，再逐条按主键更新：
// 不在 run_id 索引上做范围更新，避免可重复读下的间隙锁让不同 run 的事务互相死锁。

func (m *customWfJobModel) CancelPending(ctx context.Context, runID uint64, nodeID string, iteration int64, kinds []string) error {
	if len(kinds) == 0 {
		return nil
	}
	query := fmt.Sprintf("select `id` from %s where `run_id` = ? and `node_id` = ? and `iteration` = ? and `status` = 'pending' and `kind` in (%s)",
		m.table, strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ","))
	args := []any{runID, nodeID, iteration}
	for _, k := range kinds {
		args = append(args, k)
	}
	return m.cancelIDs(ctx, query, args...)
}

func (m *customWfJobModel) CancelRun(ctx context.Context, runID uint64) error {
	query := fmt.Sprintf("select `id` from %s where `run_id` = ? and `status` = 'pending' and `kind` <> 'sync_task'", m.table)
	return m.cancelIDs(ctx, query, runID)
}

func (m *customWfJobModel) cancelIDs(ctx context.Context, query string, args ...any) error {
	var ids []uint64
	if err := m.conn.QueryRowsCtx(ctx, &ids, query, args...); err != nil {
		return err
	}
	update := fmt.Sprintf("update %s set `status` = 'cancelled' where `id` = ? and `status` = 'pending'", m.table)
	for _, id := range ids {
		if _, err := m.conn.ExecCtx(ctx, update, id); err != nil {
			return err
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
