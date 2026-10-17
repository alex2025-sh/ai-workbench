package engine

import (
	"sync"
	"time"
)

// SystemClock 返回当前 UTC 时间，截断到毫秒，与 datetime(3) 列的精度一致。
type SystemClock struct{}

// Now 实现 Clock。
func (SystemClock) Now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// FakeClock 是可快进的时钟，用于测试与试运行。
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock 创建一个从 t 开始的时钟。
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t.UTC().Truncate(time.Millisecond)}
}

// Now 实现 Clock。
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance 把时钟向前拨 d。
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d).Truncate(time.Millisecond)
}
