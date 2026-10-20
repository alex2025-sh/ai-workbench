package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskTypeModel = (*customTaskTypeModel)(nil)

type (
	// TaskTypeModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskTypeModel.
	TaskTypeModel interface {
		taskTypeModel
		WithSession(session sqlx.Session) TaskTypeModel
		// FindOneForUpdate 在事务内锁住任务类型行（发布时用）。
		FindOneForUpdate(ctx context.Context, key string) (*TaskType, error)
		// List 按关键字（key 或名称）与 namespace 过滤，按 key 排序。
		List(ctx context.Context, keyword, namespace string) ([]*TaskType, error)
		// SaveDraft 只在 draft_revision 等于 revision 时写入草稿并加一，返回是否写入。
		SaveDraft(ctx context.Context, key string, revision int64, draft, actor string) (bool, error)
		// ClearDraft 删除草稿。
		ClearDraft(ctx context.Context, key string) error
		// SetPublished 发布后更新最新版本、名称与 namespace，并删除草稿。
		SetPublished(ctx context.Context, key string, version int64, name, namespace string) error
	}

	customTaskTypeModel struct {
		*defaultTaskTypeModel
	}
)

// NewTaskTypeModel returns a model for the database table.
func NewTaskTypeModel(conn sqlx.SqlConn) TaskTypeModel {
	return &customTaskTypeModel{
		defaultTaskTypeModel: newTaskTypeModel(conn),
	}
}

func (m *customTaskTypeModel) WithSession(session sqlx.Session) TaskTypeModel {
	return NewTaskTypeModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTaskTypeModel) FindOneForUpdate(ctx context.Context, key string) (*TaskType, error) {
	var out TaskType
	err := m.conn.QueryRowCtx(ctx, &out, fmt.Sprintf("select %s from %s where `type_key` = ? for update", taskTypeRows, m.table), key)
	if err == sqlx.ErrNotFound {
		return nil, ErrNotFound
	}
	return &out, err
}

func (m *customTaskTypeModel) List(ctx context.Context, keyword, namespace string) ([]*TaskType, error) {
	where, args := []string{"1 = 1"}, []any{}
	if keyword != "" {
		where = append(where, "(`type_key` like ? or `name` like ?)")
		args = append(args, "%"+keyword+"%", "%"+keyword+"%")
	}
	if namespace != "" {
		where = append(where, "`namespace` = ?")
		args = append(args, namespace)
	}
	var out []*TaskType
	q := fmt.Sprintf("select %s from %s where %s order by `type_key`", taskTypeRows, m.table, strings.Join(where, " and "))
	return out, m.conn.QueryRowsCtx(ctx, &out, q, args...)
}

func (m *customTaskTypeModel) SaveDraft(ctx context.Context, key string, revision int64, draft, actor string) (bool, error) {
	q := fmt.Sprintf("update %s set `draft` = ?, `draft_revision` = `draft_revision` + 1, `draft_updated_by` = ? where `type_key` = ? and `draft_revision` = ?", m.table)
	return affected(m.conn.ExecCtx(ctx, q, draft, actor, key, revision))
}

func (m *customTaskTypeModel) ClearDraft(ctx context.Context, key string) error {
	_, err := m.conn.ExecCtx(ctx, fmt.Sprintf("update %s set `draft` = null, `draft_revision` = `draft_revision` + 1 where `type_key` = ?", m.table), key)
	return err
}

func (m *customTaskTypeModel) SetPublished(ctx context.Context, key string, version int64, name, namespace string) error {
	q := fmt.Sprintf("update %s set `latest_version` = ?, `name` = ?, `namespace` = ?, `draft` = null, `draft_revision` = `draft_revision` + 1 where `type_key` = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, q, version, name, namespace, key)
	return err
}

// affected 返回 UPDATE 是否改到了行。
func affected(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
