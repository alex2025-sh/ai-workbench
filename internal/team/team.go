// Package team 是团队与成员：节点的处理人可以是团队（认领模式或分配模式）。
//
// 角色只有两种：分配者（assigner，同时也是执行者）与执行者（executor）。
// 团队成员变化即时生效，不走任务类型版本。
package team

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/model"
)

// 成员角色。
const (
	RoleAssigner = "assigner"
	RoleExecutor = "executor"
)

// ErrNotFound 表示团队不存在。
var ErrNotFound = errors.New("team: not found")

// Member 是团队成员。
type Member struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// Team 是一个团队。
type Team struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	ChatID    string   `json:"chat_id,omitempty"`
	Members   []Member `json:"members"`
	UpdatedBy string   `json:"updated_by,omitempty"`
}

// Assigners 返回分配者。
func (t *Team) Assigners() []string {
	return t.users(func(m Member) bool { return m.Role == RoleAssigner })
}

// Executors 返回全部可以执行的人（分配者也是执行者）。
func (t *Team) Executors() []string { return t.users(func(Member) bool { return true }) }

// Role 返回用户在团队中的角色，不是成员时为空。
func (t *Team) Role(user string) string {
	for _, m := range t.Members {
		if m.UserID == user {
			return m.Role
		}
	}
	return ""
}

func (t *Team) users(keep func(Member) bool) []string {
	var out []string
	for _, m := range t.Members {
		if keep(m) {
			out = append(out, m.UserID)
		}
	}
	return out
}

// Store 读写团队。
type Store struct {
	conn    sqlx.SqlConn
	teams   model.TeamModel
	members model.TeamMemberModel
}

// NewStore 创建 Store。
func NewStore(conn sqlx.SqlConn) *Store {
	return &Store{conn: conn, teams: model.NewTeamModel(conn), members: model.NewTeamMemberModel(conn)}
}

// Get 返回团队及成员。
func (s *Store) Get(ctx context.Context, key string) (*Team, error) {
	row, err := s.teams.FindOneByTeamKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, err
	}
	ms, err := s.members.ListByTeam(ctx, key)
	if err != nil {
		return nil, err
	}
	t := &Team{Key: row.TeamKey, Name: row.Name, ChatID: row.ChatId, UpdatedBy: row.UpdatedBy}
	for _, m := range ms {
		t.Members = append(t.Members, Member{UserID: m.UserId, Role: m.Role})
	}
	return t, nil
}

// List 返回全部团队及成员。
func (s *Store) List(ctx context.Context) ([]*Team, error) {
	rows, err := s.teams.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*Team, 0, len(rows))
	for _, r := range rows {
		t, err := s.Get(ctx, r.TeamKey)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// RolesOf 返回用户所在的团队及角色：团队 key → 角色。
func (s *Store) RolesOf(ctx context.Context, user string) (map[string]string, error) {
	rows, err := s.members.ListByUser(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.TeamKey] = r.Role
	}
	return out, nil
}

// Exists 报告团队是否存在。
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.teams.FindOneByTeamKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Validate 检查团队配置。
func Validate(t *Team) error {
	switch {
	case !keyPattern.MatchString(t.Key):
		return fmt.Errorf("团队 key 只能用小写字母、数字、下划线，字母开头")
	case t.Name == "":
		return fmt.Errorf("团队名称必填")
	case len(t.Members) == 0:
		return fmt.Errorf("团队至少要有一个成员")
	}
	seen := map[string]bool{}
	for _, m := range t.Members {
		if m.UserID == "" {
			return fmt.Errorf("成员不能为空")
		}
		if seen[m.UserID] {
			return fmt.Errorf("成员 %s 重复", m.UserID)
		}
		seen[m.UserID] = true
		if !slices.Contains([]string{RoleAssigner, RoleExecutor}, m.Role) {
			return fmt.Errorf("成员 %s 的角色只能是 assigner 或 executor", m.UserID)
		}
	}
	return nil
}

// Save 新建或整体替换团队（含成员）。
func (s *Store) Save(ctx context.Context, t *Team, actor string) error {
	if err := Validate(t); err != nil {
		return err
	}
	return s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		tm, mm := s.teams.WithSession(session), s.members.WithSession(session)
		row, err := tm.FindOneByTeamKey(ctx, t.Key)
		switch {
		case errors.Is(err, model.ErrNotFound):
			if _, err := tm.Insert(ctx, &model.Team{TeamKey: t.Key, Name: t.Name, ChatId: t.ChatID, UpdatedBy: actor}); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			row.Name, row.ChatId, row.UpdatedBy = t.Name, t.ChatID, actor
			if err := tm.Update(ctx, row); err != nil {
				return err
			}
		}
		if err := mm.DeleteByTeam(ctx, t.Key); err != nil {
			return err
		}
		for _, m := range t.Members {
			if _, err := mm.Insert(ctx, &model.TeamMember{TeamKey: t.Key, UserId: m.UserID, Role: m.Role}); err != nil {
				return err
			}
		}
		return nil
	})
}
