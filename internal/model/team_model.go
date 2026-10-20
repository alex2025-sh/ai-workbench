package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TeamModel = (*customTeamModel)(nil)

type (
	// TeamModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTeamModel.
	TeamModel interface {
		teamModel
		WithSession(session sqlx.Session) TeamModel
		// List 返回全部团队，按 team_key 排序。
		List(ctx context.Context) ([]*Team, error)
	}

	customTeamModel struct {
		*defaultTeamModel
	}
)

// NewTeamModel returns a model for the database table.
func NewTeamModel(conn sqlx.SqlConn) TeamModel {
	return &customTeamModel{
		defaultTeamModel: newTeamModel(conn),
	}
}

func (m *customTeamModel) WithSession(session sqlx.Session) TeamModel {
	return NewTeamModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTeamModel) List(ctx context.Context) ([]*Team, error) {
	var out []*Team
	return out, m.conn.QueryRowsCtx(ctx, &out, fmt.Sprintf("select %s from %s order by `team_key`", teamRows, m.table))
}
