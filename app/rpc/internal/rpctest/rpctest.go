// Package rpctest 为 RPC logic 的集成测试准备环境：临时库、发布 VIP 示例、触发一次扫描。
// 需要 WB_TEST_MYSQL，未设置时测试跳过（见 internal/pkg/testdb）。
package rpctest

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
)

// Env 是一套跑过首次扫描的环境：A1001、A1002 在 rm.alice 名下；A1007 在 vip_cs_leads 认领队列；
// A1003 在 vip_review 分配队列（分配者 lead@example.com）。
type Env struct {
	*bootstrap.TestEnv
	Svc *svc.ServiceContext
}

// New 准备环境。
func New(t *testing.T) *Env {
	t.Helper()
	e := bootstrap.NewTestEnv(t)
	e.Advance(time.Minute)
	if err := e.Ticker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &Env{TestEnv: e, Svc: &svc.ServiceContext{Services: e.Services}}
}

// As 返回带调用人身份的 context（模拟 gateway 写入的 metadata）。
func As(user string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(identity.MetadataKey, user))
}
