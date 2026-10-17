package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
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
		if j.Attempt > e.opts.MaxClaims {
			// 反复认领却从未完成：多半是处理时进程崩溃（如内存耗尽），继续重试只会反复崩溃。
			e.quarantine(ctx, j, fmt.Sprintf("claimed %d times without finishing", j.Attempt))
			continue
		}
		if e.afterClaim != nil {
			e.afterClaim(j)
		}
		next, err := e.process(ctx, j)
		if err != nil {
			logx.WithContext(ctx).Errorf("engine: process job %d (%s %s): %v", j.ID, j.Kind, j.NodeID, err)
			e.failed(ctx, j, err)
			continue
		}
		queue = append(queue, e.due(next)...)
	}
	return n
}

// failed 处理一次失败的处理（加载定义、提交事务出错等，事务已回滚）。
//   - 调用方的 ctx 已取消（如进程退出）：立即释放 job，不计失败次数，别的进程不必等租约过期；
//   - 否则退避后重试并计数，连续失败达到 MaxFailCount 时隔离，避免确定性错误无限重试。
func (e *Engine) failed(ctx context.Context, j Job, cause error) {
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var err error
	switch {
	case ctx.Err() != nil:
		_, err = e.store.jobs.Release(bg, uint64(j.ID), j.Owner, int64(j.Attempt), e.clock.Now(), "released: "+ctx.Err().Error())
	case j.FailCount+1 >= e.opts.MaxFailCount:
		e.quarantine(bg, j, fmt.Sprintf("failed %d times: %v", j.FailCount+1, cause))
		return
	default:
		policy := dsl.RetryPolicy{InitialInterval: dsl.Duration(2 * time.Second), BackoffCoefficient: 2, MaxInterval: dsl.Duration(5 * time.Minute)}
		_, err = e.store.jobs.Backoff(bg, uint64(j.ID), j.Owner, int64(j.Attempt), e.clock.Now().Add(backoff(policy, j.FailCount+1)), cause.Error())
	}
	if err != nil {
		logx.WithContext(ctx).Errorf("engine: release job %d: %v", j.ID, err)
	}
}

// quarantine 隔离一个毒丸 job：job 置为 failed，run 仍在运行时转为 needs_intervention，并记录事件。
// 不经过 load：定义加载失败、变量损坏这类问题正是隔离的原因。
func (e *Engine) quarantine(ctx context.Context, j Job, reason string) {
	err := e.store.Tx(ctx, func(t *tx) error {
		run, err := t.runs.FindOneForUpdate(t.ctx, uint64(j.RunID))
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if err := t.finishJob(j, JobFailed, "quarantined: "+reason); err != nil {
			return err
		}
		if run != nil && run.Status == string(RunRunning) {
			if err := t.runs.UpdateState(t.ctx, run.Id, string(RunNeedsIntervention), run.Version+1, run.ExecCount); err != nil {
				return err
			}
		}
		return t.event(j.RunID, j.NodeID, j.Iteration, "job_quarantined", "", map[string]any{
			"job_id": j.ID, "kind": j.Kind, "attempt": j.Attempt, "fail_count": j.FailCount, "reason": reason,
		})
	})
	if errors.Is(err, errLostLease) {
		return // 已被别的认领接手，由对方处理
	}
	if err != nil {
		logx.WithContext(ctx).Errorf("engine: quarantine job %d: %v", j.ID, err)
		return
	}
	logx.WithContext(ctx).Errorf("engine: job %d (run %d %s %s) quarantined: %s", j.ID, j.RunID, j.Kind, j.NodeID, reason)
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
