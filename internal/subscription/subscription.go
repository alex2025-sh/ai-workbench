// Package subscription 是订阅路由：事件类型 + 过滤条件 → 任务类型，并完成 payload 映射与去重键计算。
package subscription

import (
	"context"
	"encoding/json"

	"github.com/alex2025-sh/ai-workbench/internal/event"
)

// Subscription 把某类事件转成某类任务。
type Subscription struct {
	ID          string            `json:"id"`
	Version     int               `json:"version"`
	EventType   string            `json:"event_type"`
	Filter      string            `json:"filter,omitempty"` // 表达式，如 segment == 'vip'
	TaskType    string            `json:"task_type"`
	Mapping     map[string]string `json:"mapping"`            // task 字段 -> JSONPath
	DedupKey    string            `json:"dedup_key"`          // 模板，如 {{ticket_id}}:48h
	Priority    string            `json:"priority,omitempty"` // 表达式；省略则用任务类型默认优先级
	Description string            `json:"description,omitempty"`
}

// CreateTaskRequest 是路由的产出，交给任务中心创建任务并启动 workflow。
type CreateTaskRequest struct {
	TaskType string
	Subject  event.Subject
	Payload  json.RawMessage
	DedupKey string
	Priority *int16
}

// Router 匹配订阅并生成任务创建请求。
type Router interface {
	Route(ctx context.Context, e event.Envelope) ([]CreateTaskRequest, error)
}
