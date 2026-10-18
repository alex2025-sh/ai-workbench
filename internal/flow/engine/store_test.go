package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/testdb"
)

// TestTransactDropsRolledBackResult：第一次尝试准备了结果后死锁回滚，第二次走了提前返回的分支；
// transact 只能返回第二次的结果，第一次的写入与结果都不能留下。
func TestTransactDropsRolledBackResult(t *testing.T) {
	conn := sqlx.NewMysql(testdb.New(t, "deploy/sql/*.sql"))
	s := NewStore(conn)
	ctx := context.Background()
	attempts := 0
	got, err := transact(ctx, s, func(t *tx) (string, error) {
		attempts++
		if attempts == 1 {
			if _, err := t.evts.Insert(t.ctx, &model.WfEvent{RunId: 1, Type: "first_attempt", Actor: "system"}); err != nil {
				return "", err
			}
			return "prepared", &mysql.MySQLError{Number: 1213, Message: "Deadlock found"}
		}
		return "", nil // 第二次：状态已变化，不再准备
	})
	if err != nil || got != "" || attempts != 2 {
		t.Fatalf("got=%q err=%v attempts=%d", got, err, attempts)
	}
	evs, err := s.Events(ctx, 1)
	if err != nil || len(evs) != 0 {
		t.Fatalf("rolled back write leaked: %v %v", evs, err)
	}

	boom := errors.New("boom")
	if _, err := transact(ctx, s, func(*tx) (int, error) { return 1, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
