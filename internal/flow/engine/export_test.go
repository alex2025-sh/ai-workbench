package engine

import "context"

// StepForTest 处理一个指定的 job，供外部测试构造并发交错（多个 tick 取到同一批候选）。
func (e *Engine) StepForTest(ctx context.Context, j Job) { e.step(ctx, j) }
