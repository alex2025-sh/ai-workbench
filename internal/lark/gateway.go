// Package lark 是 Lark 网关：发送与更新卡片、回调验签、open_id 与内部用户的映射。
//
// 回调处理顺序：验签 → open_id 映射为内部用户 → 按任务表鉴权（是否为当前处理人）→ 调用引擎 Signal。
// 任务状态变化时，通过 task_id → message_id 映射更新原卡片，避免处理人点到过期卡片。
package lark

import (
	"context"
	"encoding/json"
	"net/http"
)

// CardRef 指向一张已发送的卡片。
type CardRef struct {
	TaskID    int64
	MessageID string
}

// CallbackAction 是从卡片回调中解析出的一次点击。
type CallbackAction struct {
	OpenID    string
	RunID     int64
	NodeID    string
	Iteration int
	Option    string
	Form      json.RawMessage
}

// Gateway 封装 Lark 开放平台的调用。
type Gateway interface {
	// SendCard 发送卡片；idemKey 防止重试导致重复发送。
	SendCard(ctx context.Context, receiverOpenID string, card json.RawMessage, idemKey string) (CardRef, error)

	// UpdateCard 更新已发送的卡片，如“已由他人处理”“已升级”“AI 摘要已生成”。
	UpdateCard(ctx context.Context, ref CardRef, card json.RawMessage) error

	// ParseCallback 验签并解析卡片交互回调。
	ParseCallback(r *http.Request) (*CallbackAction, error)
}

// Identity 在 Lark open_id 与内部用户之间映射。
type Identity interface {
	UserIDByOpenID(ctx context.Context, openID string) (string, error)
	OpenIDByUserID(ctx context.Context, userID string) (string, error)
}
