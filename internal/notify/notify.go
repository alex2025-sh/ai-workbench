// Package notify 投递 Lark 通知：状态转移在同一事务里写 outbox，提交后立即投递，失败由 ticker 退避重试。
//
// 一期只发通知、不接卡片回调：卡片上只有 URL 按钮（「在工作台处理」与业务链接），所有动作在工作台完成。
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// 投递目标的类型。
const (
	KindUser = "lark_user" // 私聊发给一个人（目标是用户 id）
	KindChat = "lark_chat" // 发到群（目标是 chat_id）
)

// Sender 发送 Lark 卡片。idemKey 相同的重复发送只发一次（Lark 的 uuid 参数）。
type Sender interface {
	Send(ctx context.Context, kind, target string, card tasktype.RenderedCard, idemKey string) error
}

// Dispatcher 投递 outbox 中的记录。
type Dispatcher struct {
	Outbox model.OutboxModel
	Sender Sender
	Now    func() time.Time

	// Inline 为 true 时 Kick 在调用方的 goroutine 里同步投递（测试用）。
	Inline bool

	wg sync.WaitGroup
}

// 重试策略：1 分钟起，每次翻倍，最多 6 小时；10 次后放弃。
const (
	maxAttempts = 10
	leaseFor    = 2 * time.Minute
)

// Kick 在状态转移提交后调用，立即投递新写入的记录。投递失败不影响调用方，由 RetryDue 重试。
func (d *Dispatcher) Kick(ids []uint64) {
	if len(ids) == 0 {
		return
	}
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, id := range ids {
			if err := d.Deliver(ctx, id); err != nil {
				logx.WithContext(ctx).Errorf("notify: outbox %d: %v", id, err)
			}
		}
	}
	if d.Inline {
		run()
		return
	}
	d.wg.Add(1)
	go func() { defer d.wg.Done(); run() }()
}

// Wait 等待进行中的后台投递结束（进程退出前调用）。
func (d *Dispatcher) Wait() { d.wg.Wait() }

// RetryDue 投递到期的记录，返回处理条数。
func (d *Dispatcher) RetryDue(ctx context.Context, limit int) (int, error) {
	ids, err := d.Outbox.Due(ctx, d.Now(), limit)
	if err != nil {
		return 0, err
	}
	var firstErr error
	for _, id := range ids {
		if err := d.Deliver(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(ids), firstErr
}

// Deliver 投递一条记录：先抢占（推迟 next_at），发送，再标记结果。抢不到说明别的实例在投递或已完成。
func (d *Dispatcher) Deliver(ctx context.Context, id uint64) error {
	now := d.Now()
	ok, err := d.Outbox.Claim(ctx, id, now, now.Add(leaseFor))
	if err != nil || !ok {
		return err
	}
	o, err := d.Outbox.FindOne(ctx, id)
	if err != nil {
		return err
	}
	var card tasktype.RenderedCard
	if err := json.Unmarshal([]byte(o.Payload), &card); err != nil {
		return d.Outbox.Failed(ctx, id, "bad payload: "+err.Error(), now, true)
	}
	if err := d.Sender.Send(ctx, o.Kind, o.Target, card, o.IdemKey); err != nil {
		dead := o.Attempts >= maxAttempts || errors.Is(err, ErrPermanent)
		backoff := time.Minute << min(o.Attempts-1, 9)
		if backoff > 6*time.Hour {
			backoff = 6 * time.Hour
		}
		if ferr := d.Outbox.Failed(ctx, id, err.Error(), now.Add(backoff), dead); ferr != nil {
			return ferr
		}
		return err
	}
	return d.Outbox.Done(ctx, id)
}

// ErrPermanent 表示重试也不会成功（如接收人不存在），直接放弃。
var ErrPermanent = errors.New("notify: permanent failure")

// Sent 是 Mock 记录的一次发送。
type Sent struct {
	Kind, Target, IdemKey string
	Card                  tasktype.RenderedCard
}

// Mock 不连 Lark：把卡片打到日志并记在内存里，同一个幂等键只记一次。
type Mock struct {
	mu   sync.Mutex
	sent []Sent
	keys map[string]bool

	// Fail 不为 nil 时在发送前调用，返回错误则本次发送失败（测试用）。
	Fail func(kind, target string) error
}

// Send 实现 Sender。
func (m *Mock) Send(ctx context.Context, kind, target string, card tasktype.RenderedCard, idemKey string) error {
	if m.Fail != nil {
		if err := m.Fail(kind, target); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil {
		m.keys = map[string]bool{}
	}
	if m.keys[idemKey] {
		return nil
	}
	m.keys[idemKey] = true
	m.sent = append(m.sent, Sent{Kind: kind, Target: target, IdemKey: idemKey, Card: card})
	logx.WithContext(ctx).Infof("lark(mock) → %s %s\n%s", kind, target, Text(card))
	return nil
}

// Sent 返回已发送的卡片（副本）。
func (m *Mock) Sent() []Sent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Sent(nil), m.sent...)
}

// Text 把卡片渲染成便于阅读的文本。
func Text(c tasktype.RenderedCard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【%s】%s\n", c.NodeName, c.Title)
	for _, f := range c.Fields {
		fmt.Fprintf(&b, "  %s：%s\n", f.Label, f.Value)
	}
	if c.TaskURL != "" {
		fmt.Fprintf(&b, "  [在工作台处理](%s)", c.TaskURL)
	}
	if c.Link != nil {
		fmt.Fprintf(&b, "  [%s](%s)", c.Link.Label, c.Link.Value)
	}
	return b.String()
}
