// Package ticker 是 xxl-job 驱动的外部时钟。
//
// 只在 xxl-job 注册一个调度任务 wf-ticker（每 10~30 秒），状态与逻辑全部在引擎内：
//  1. 扫描 wf_schedule，触发到期的 cron 来源并用乐观锁推进 next_fire_at；
//  2. 执行到期的 job：timer / timeout / 退避后的重试 / 租约过期的 job；
//  3. 巡检长时间 waiting 或 needs_intervention 的 run 并告警（待实现）。
//
// Tick 必须幂等：重复触发无副作用，漏触发由下一次补上。
package ticker

import "context"

// JobName 是在 xxl-job 中注册的任务名。
const JobName = "wf-ticker"

// Shard 对应 xxl-job 的分片广播参数；实例多了之后按 run_id 取模分担扫描。
type Shard struct {
	Index int
	Total int
}

// Handler 处理一次时钟触发。
type Handler interface {
	Tick(ctx context.Context, shard Shard) error
}
