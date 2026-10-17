// Package event 是事件中心：事件类型注册、按 schema 校验、按幂等键去重、分发。
//
// 任何来源（cron 扫描、Kafka、API 推送）的数据先归一成标准事件信封，再进入订阅路由。
package event

import (
	"context"
	"encoding/json"
	"time"
)

// Envelope 是标准事件信封。
type Envelope struct {
	Type     string          `json:"type"` // 如 ticket.created、ticket.maker_overdue
	Subject  Subject         `json:"subject"`
	Time     time.Time       `json:"time"`
	Payload  json.RawMessage `json:"payload"`
	DedupKey string          `json:"dedup_key"` // 多个来源共用，如 ticket_id:48h
	Source   string          `json:"source"`    // cron:<schedule> / kafka:<topic> / api:<client>
}

// Subject 是事件主体。
type Subject struct {
	Type string `json:"type"` // vip / ticket / account
	ID   string `json:"id"`
}

// Type 是一个已注册的事件类型。
type Type struct {
	Name        string
	SchemaRef   string // payload schema 引用
	Owner       string
	Sensitivity string // 数据敏感等级
}

// Hub 接收并分发事件。
type Hub interface {
	// Publish 校验并去重后分发；重复的 DedupKey 返回 duplicate=true 且不报错。
	Publish(ctx context.Context, e Envelope) (duplicate bool, err error)
}
