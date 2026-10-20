package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TaskParticipantModel = (*customTaskParticipantModel)(nil)

type (
	// TaskParticipantModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskParticipantModel.
	TaskParticipantModel interface {
		taskParticipantModel
		WithSession(session sqlx.Session) TaskParticipantModel
		// Add 记录经手人，已存在时忽略。
		Add(ctx context.Context, taskID uint64, userID string) error
	}

	customTaskParticipantModel struct {
		*defaultTaskParticipantModel
	}
)

// NewTaskParticipantModel returns a model for the database table.
func NewTaskParticipantModel(conn sqlx.SqlConn) TaskParticipantModel {
	return &customTaskParticipantModel{
		defaultTaskParticipantModel: newTaskParticipantModel(conn),
	}
}

func (m *customTaskParticipantModel) WithSession(session sqlx.Session) TaskParticipantModel {
	return NewTaskParticipantModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customTaskParticipantModel) Add(ctx context.Context, taskID uint64, userID string) error {
	_, err := m.conn.ExecCtx(ctx, fmt.Sprintf("insert ignore into %s (`task_id`, `user_id`) values (?, ?)", m.table), taskID, userID)
	return err
}
