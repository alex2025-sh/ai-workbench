package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
)

// SentCard 是 Mock 记录的一次发送。
type SentCard struct {
	MessageID string
	IdemKey   string
	To        action.AssignOutput
	Card      action.Card
}

// Mock 是不连 Lark 的卡片发送实现，注册为 action.KindLarkCard：把卡片打到日志并记在内存里。
// 同一个幂等键只“发送”一次，重试返回同一个 message_id。
type Mock struct {
	mu   sync.Mutex
	sent []SentCard
	keys map[string]string

	// BeforeSend 在发送前调用，返回错误时本次发送失败（测试用：注入故障、观察发送时的流程状态）。
	BeforeSend func(ctx context.Context, req action.Request) error
}

// NewMock 创建 Mock。
func NewMock() *Mock { return &Mock{keys: map[string]string{}} }

// Kind 实现 action.Action。
func (*Mock) Kind() action.Kind { return action.KindLarkCard }

// Invoke 实现 action.Action。
func (m *Mock) Invoke(ctx context.Context, req action.Request) (action.Result, error) {
	var in action.LarkCardInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return action.Result{}, fmt.Errorf("lark: input: %w", err)
	}
	if m.BeforeSend != nil {
		if err := m.BeforeSend(ctx, req); err != nil {
			return action.Result{}, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, dup := m.keys[req.IdemKey]
	if !dup {
		id = fmt.Sprintf("mock_msg_%d", len(m.sent)+1)
		m.keys[req.IdemKey] = id
		m.sent = append(m.sent, SentCard{MessageID: id, IdemKey: req.IdemKey, To: in.To, Card: in.Card})
		logx.WithContext(ctx).Infof("lark(mock) → %s\n%s", recipient(in.To), Text(in.Card))
	}
	out, _ := json.Marshal(action.LarkCardOutput{MessageID: id})
	return action.Result{Output: out}, nil
}

// Sent 返回已发送的卡片（副本）。
func (m *Mock) Sent() []SentCard {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SentCard(nil), m.sent...)
}

func recipient(to action.AssignOutput) string {
	if to.Assignee != "" {
		return to.Assignee
	}
	return "group:" + to.Group
}

// Text 把卡片渲染成便于阅读的文本。
func Text(c action.Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【%s】%s\n", c.Color, c.Title)
	if c.Summary != "" {
		fmt.Fprintf(&b, "%s\n", c.Summary)
	}
	for _, f := range c.Fields {
		fmt.Fprintf(&b, "  %s：%s\n", f.Label, f.Value)
	}
	for _, a := range c.Actions {
		fmt.Fprintf(&b, "  [%s]", a.Label)
	}
	if len(c.Actions) > 0 {
		b.WriteString("\n")
	}
	return b.String()
}
