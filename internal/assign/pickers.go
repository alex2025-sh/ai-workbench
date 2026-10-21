package assign

import (
	"context"
	"math/rand/v2"
)

// Random 随机选一个。
type Random struct{}

// Strategy 实现 Picker。
func (Random) Strategy() string { return "random" }

// Pick 实现 Picker。
func (Random) Pick(_ context.Context, users []string, _ string) (string, error) {
	return users[rand.IntN(len(users))], nil
}
