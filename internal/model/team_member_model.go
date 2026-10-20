package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TeamMemberModel = (*customTeamMemberModel)(nil)

type (
	// TeamMemberModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTeamMemberModel.
	TeamMemberModel interface {
		teamMemberModel
		WithSession(session sqlx.Session) TeamMemberModel
		// ListByTeam 返回团队的全部成员，按 user_id 排序。
		ListByTeam(ctx context.Context, teamKey string) ([]*TeamMember, error)
		// ListByUser 返回用户所在的全部团队成员行。
		ListByUser(ctx context.Context, userID string) ([]*TeamMember, error)
		// DeleteByTeam 删除团队的全部成员（保存团队时整体替换）。
		DeleteByTeam(ctx context.Context, teamKey string) error
	}

	customTeamMemberModel struct {
		*defaultTeamMemberModel
	}
)

// NewTeamMemberModel returns a model for the database table.
func NewTeamMemberModel(conn sqlx.SqlConn) TeamMemberModel {
	return &customTeamMemberModel{
		defaultTeamMemberModel: newTeamMemberModel(conn),
	}
}

func (m *customTeamMemberModel) WithSession(session sqlx.Session) TeamMemberModel {
	return NewTeamMemberModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTeamMemberModel) ListByTeam(ctx context.Context, teamKey string) ([]*TeamMember, error) {
	var out []*TeamMember
	q := fmt.Sprintf("select %s from %s where `team_key` = ? order by `user_id`", teamMemberRows, m.table)
	return out, m.conn.QueryRowsCtx(ctx, &out, q, teamKey)
}

func (m *customTeamMemberModel) ListByUser(ctx context.Context, userID string) ([]*TeamMember, error) {
	var out []*TeamMember
	q := fmt.Sprintf("select %s from %s where `user_id` = ? order by `team_key`", teamMemberRows, m.table)
	return out, m.conn.QueryRowsCtx(ctx, &out, q, userID)
}

func (m *customTeamMemberModel) DeleteByTeam(ctx context.Context, teamKey string) error {
	_, err := m.conn.ExecCtx(ctx, fmt.Sprintf("delete from %s where `team_key` = ?", m.table), teamKey)
	return err
}
