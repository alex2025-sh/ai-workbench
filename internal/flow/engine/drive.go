package engine

import (
	"context"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// drive 就地推进：依次认领并执行已到期的 job，以及执行过程中新就绪的 job，直到遇到等待点或结束。
//
// 这里没有队列和执行池：触发推进的那次调用（StartRun、Signal、Cancel 或一次 tick）自己把后续的自动节点跑完。
// 每个 job 仍然先落库、再认领、再执行，所以进程在中途崩溃时，剩下的 job 会在租约过期后由下一次 tick 接着执行。
func (e *Engine) drive(ctx context.Context, jobs []Job) int {
	queue := e.due(jobs)
	n := 0
	for len(queue) > 0 && ctx.Err() == nil {
		j := queue[0]
		queue = queue[1:]
		ok, err := e.store.claim(ctx, &j, e.opts.Owner, e.clock.Now(), e.opts.Lease)
		if err != nil {
			logx.WithContext(ctx).Errorf("engine: claim job %d: %v", j.ID, err)
			continue
		}
		if !ok {
			continue // 已被别的调用认领或已完成
		}
		n++
		next, err := e.process(ctx, j)
		if err != nil {
			// 提交失败时事务已回滚，job 仍处于认领状态，租约过期后由 tick 重新执行。
			logx.WithContext(ctx).Errorf("engine: process job %d (%s %s): %v", j.ID, j.Kind, j.NodeID, err)
			continue
		}
		queue = append(queue, e.due(next)...)
	}
	return n
}

// due 过滤出已到期的 job；未到期的（定时器、超时、退避中的重试）留给 tick。
func (e *Engine) due(jobs []Job) []Job {
	now := e.clock.Now()
	var out []Job
	for _, j := range jobs {
		if !j.DueAt.After(now) {
			out = append(out, j)
		}
	}
	return out
}

// TickOptions 控制一次 tick 处理多少到期 job。
type TickOptions struct {
	BatchSize   int // 每批读取的到期 job 数，默认 100
	MaxBatches  int // 一次 tick 最多处理的批数，默认 20；剩下的留给下一次 tick
	Parallelism int // 并行推进的 job 数，默认 4；同一个 run 的推进仍由 run 行锁串行化
}

// ProcessDue 由 xxl-job 的 tick 调用：执行全部到期的 job（定时器、超时、退避后的重试、租约过期的 job），
// 每个 job 执行后同样就地推进。返回执行的 job 数。
func (e *Engine) ProcessDue(ctx context.Context, opts TickOptions) (int, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.MaxBatches <= 0 {
		opts.MaxBatches = 20
	}
	if opts.Parallelism <= 0 {
		opts.Parallelism = 4
	}
	total := 0
	for b := 0; b < opts.MaxBatches && ctx.Err() == nil; b++ {
		jobs, err := e.store.dueJobs(ctx, e.clock.Now(), opts.BatchSize)
		if err != nil {
			return total, err
		}
		if len(jobs) == 0 {
			break
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, opts.Parallelism)
		n := 0
		for _, j := range jobs {
			wg.Add(1)
			sem <- struct{}{}
			go func(j Job) {
				defer func() { <-sem; wg.Done() }()
				k := e.drive(ctx, []Job{j})
				mu.Lock()
				n += k
				mu.Unlock()
			}(j)
		}
		wg.Wait()
		total += n
		if n == 0 || len(jobs) < opts.BatchSize {
			break // 剩下的都被别人持有，或已经处理完
		}
	}
	return total, nil
}
