// Package ingest 是接入适配器：把 cron 扫描、Kafka 消费、API / Webhook 推送归一成事件信封。
//
// cron 来源优先使用 Skillhub 中已认证的查询 skill；AI 现写的查询经审核后沉淀为新的 skill。
// 查询只读、走只读副本，保存前做成本检查，运行时有超时与行数上限。
package ingest

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/internal/event"
)

// SourceKind 是来源类型。
type SourceKind string

const (
	SourceCron  SourceKind = "cron"
	SourceKafka SourceKind = "kafka"
	SourceAPI   SourceKind = "api"
)

// Emitter 把归一后的事件交给事件中心。
type Emitter interface {
	Emit(ctx context.Context, e event.Envelope) error
}

// Source 是一种接入适配器。
type Source interface {
	Kind() SourceKind
	Run(ctx context.Context, out Emitter) error
}
