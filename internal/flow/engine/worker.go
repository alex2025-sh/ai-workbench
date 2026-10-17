package engine

import (
	"context"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// WorkerOptions 是 Worker 池的参数。
type WorkerOptions struct {
	Concurrency  int           // 并发执行的 job 数，默认 8
	PollInterval time.Duration // 轮询到期 job 的间隔，默认 1 秒；提交后新就绪的 job 直接派发，不等轮询
}

// Worker 认领并执行 job：CAS 认领 → 事务外执行 → 交给推进器提交。
// 提交后新就绪的 job 经 Dispatch 直接进入本进程的执行队列；轮询负责定时 job 与兜底（租约过期的 job 会被重新认领）。
type Worker struct {
	e     *Engine
	opts  WorkerOptions
	queue chan Job
}

// NewWorker 创建 Worker 并注册为引擎的派发回调。
func NewWorker(e *Engine, opts WorkerOptions) *Worker {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	w := &Worker{e: e, opts: opts, queue: make(chan Job, opts.Concurrency*4)}
	e.SetDispatcher(w.Dispatch)
	return w
}

// Run 启动执行池与轮询，直到 ctx 结束。
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-w.queue:
					w.process(ctx, j)
				}
			}
		}()
	}
	ticker := time.NewTicker(w.opts.PollInterval)
	defer ticker.Stop()
	for {
		w.poll(ctx, func(j Job) bool {
			select {
			case w.queue <- j:
				return true
			case <-ctx.Done():
				return false
			}
		})
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
		}
	}
}

// Dispatch 把 job 排入本进程的执行队列，执行前再认领；队列满时留给下一次轮询。
func (w *Worker) Dispatch(jobs []Job) {
	for _, j := range jobs {
		select {
		case w.queue <- j:
		default:
		}
	}
}

// poll 取出到期且未被认领（或租约已过期）的 job 交给 handle；handle 返回 false 时停止。
func (w *Worker) poll(ctx context.Context, handle func(Job) bool) int {
	jobs, err := w.e.store.dueJobs(ctx, w.e.clock.Now(), w.opts.Concurrency*4)
	if err != nil {
		logx.WithContext(ctx).Errorf("engine: poll due jobs: %v", err)
		return 0
	}
	for i, j := range jobs {
		if !handle(j) {
			return i
		}
	}
	return len(jobs)
}

// process 用 CAS 认领后执行；认领失败说明已被别的 worker 抢走或已执行完。
func (w *Worker) process(ctx context.Context, j Job) {
	ok, err := w.e.store.claim(ctx, &j, w.e.opts.Owner, w.e.clock.Now(), w.e.opts.Lease)
	if err != nil {
		logx.WithContext(ctx).Errorf("engine: claim job %d: %v", j.ID, err)
		return
	}
	if !ok {
		return
	}
	if err := w.e.Process(ctx, j); err != nil {
		// 提交失败时事务回滚，job 仍处于认领状态，租约过期后由轮询重新认领。
		logx.WithContext(ctx).Errorf("engine: process job %d (%s %s): %v", j.ID, j.Kind, j.NodeID, err)
	}
}

// Drain 在当前 goroutine 中同步处理全部到期 job，直到没有可认领的 job。
// 用于测试、试运行与 xxl-job ticker 的兜底执行。
func (w *Worker) Drain(ctx context.Context) {
	for {
		batch := w.drainQueue()
		w.poll(ctx, func(j Job) bool { batch = append(batch, j); return true })
		if len(batch) == 0 {
			return
		}
		progressed := false
		for _, j := range batch {
			ok, err := w.e.store.claim(ctx, &j, w.e.opts.Owner, w.e.clock.Now(), w.e.opts.Lease)
			if err != nil || !ok {
				continue
			}
			progressed = true
			if err := w.e.Process(ctx, j); err != nil {
				logx.WithContext(ctx).Errorf("engine: process job %d (%s %s): %v", j.ID, j.Kind, j.NodeID, err)
			}
		}
		if !progressed {
			return // 剩下的都被别的 worker 持有
		}
	}
}

func (w *Worker) drainQueue() []Job {
	var out []Job
	for {
		select {
		case j := <-w.queue:
			out = append(out, j)
		default:
			return out
		}
	}
}
