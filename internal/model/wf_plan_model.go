package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ WfPlanModel = (*customWfPlanModel)(nil)

type (
	// WfPlanModel is an interface to be customized, add more methods here,
	// and implement the added methods in customWfPlanModel.
	WfPlanModel interface {
		wfPlanModel
		WithSession(session sqlx.Session) WfPlanModel
		// MaxVersion 返回某个任务类型已发布的最大版本，没有时为 0。
		MaxVersion(ctx context.Context, planID string) (int64, error)
		// Versions 返回某个任务类型的全部发布版本，新版本在前。
		Versions(ctx context.Context, planID string) ([]*WfPlan, error)
		// Latest 返回每个任务类型的最新发布版本，按 key 排序。
		Latest(ctx context.Context) ([]*WfPlan, error)
	}

	customWfPlanModel struct {
		*defaultWfPlanModel
	}
)

// NewWfPlanModel returns a model for the database table.
func NewWfPlanModel(conn sqlx.SqlConn) WfPlanModel {
	return &customWfPlanModel{
		defaultWfPlanModel: newWfPlanModel(conn),
	}
}

func (m *customWfPlanModel) WithSession(session sqlx.Session) WfPlanModel {
	return NewWfPlanModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customWfPlanModel) MaxVersion(ctx context.Context, planID string) (int64, error) {
	query := fmt.Sprintf("select coalesce(max(`version`), 0) from %s where `plan_id` = ?", m.table)
	var v int64
	err := m.conn.QueryRowCtx(ctx, &v, query, planID)
	return v, err
}

func (m *customWfPlanModel) Versions(ctx context.Context, planID string) ([]*WfPlan, error) {
	query := fmt.Sprintf("select %s from %s where `plan_id` = ? order by `version` desc", wfPlanRows, m.table)
	var out []*WfPlan
	err := m.conn.QueryRowsCtx(ctx, &out, query, planID)
	return out, err
}

func (m *customWfPlanModel) Latest(ctx context.Context) ([]*WfPlan, error) {
	query := fmt.Sprintf("select %s from %s p where `version` = (select max(`version`) from %s where `plan_id` = p.`plan_id`) order by `plan_id`",
		wfPlanRows, m.table, m.table)
	var out []*WfPlan
	err := m.conn.QueryRowsCtx(ctx, &out, query)
	return out, err
}
