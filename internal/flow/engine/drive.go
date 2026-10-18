package engine

import (
	"context"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// drive 就地推进：处理给定的已到期 job，以及处理过程中新就绪的 job，直到遇到等待点、结束或用完预算。
//
// 这里没有常驻队列：触发推进的那次调用（StartRun、Signal、Cancel 或一次 tick）自己把后续的自动节点跑完。
// 并行就绪的节点（分支）并发执行，同时最多 DriveParallelism 个；一次最多处理 DriveBudget 个 job，
// 剩下的留在表里由 tick 接着执行。每个 job 都先落库、再在 run 锁内认领、再执行，进程中途崩溃时由 tick 接手。
// 返回实际处理的 job 数（被别人处理或持有的不计）。
func (e *Engine) drive(ctx context.Context, jobs []Job) int {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
		n  int
	)
	sem := make(chan struct{}, e.opts.DriveParallelism)
	var spawn func(j Job)
	spawn = func(j Job) {
		mu.Lock()
		if n >= e.opts.DriveBudget {
			mu.Unlock()
			return // 超出预算：job 留在表里，由 tick 执行
		}
		n++
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			next, worked := e.step(ctx, j)
			<-sem
			if !worked {
				mu.Lock()
				n-- // 没处理，不占预算
				mu.Unlock()
			}
			for _, nj := range e.due(next) {
				spawn(nj)
			}
		}()
	}
	for _, j := range e.due(jobs) {
		if ctx.Err() != nil {
			break
		}
		spawn(j)
	}
	wg.Wait()
	return n
}

// step 处理一个 job，返回处理中新插入的 job。处理出错时记录失败，由 failed 退避或隔离。
func (e *Engine) step(ctx context.Context, j Job) (next []Job, worked bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	next, worked, err := e.process(ctx, &j)
	if err != nil {
		logx.WithContext(ctx).Errorf("engine: process job %d (%s %s): %v", j.ID, j.Kind, j.NodeID, err)
		if ctx.Err() == nil { // 进程退出导致的出错不计数：job 未被改动，或在租约到期后被接手
			e.failed(ctx, j, err)
		}
		return nil, true
	}
	return next, worked
}

// driveLater 在后台推进：脱离调用方的 ctx（请求返回后仍继续），保留其中的日志与追踪信息。
// 同时进行的后台推进有上限，满了就不起，job 留给 tick；进程退出时 Close 等待或取消它们。
func (e *Engine) driveLater(ctx context.Context, jobs []Job) {
	jobs = e.due(jobs)
	if len(jobs) == 0 {
		return
	}
	select {
	case e.bgSem <- struct{}{}:
	default:
		logx.WithContext(ctx).Infof("engine: background drive limit reached, %d jobs left for the ticker", len(jobs))
		return
	}
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.opts.BackgroundTimeout)
	stop := context.AfterFunc(e.bgCtx, cancel)
	e.bgWG.Add(1)
	go func() {
		defer func() { stop(); cancel(); <-e.bgSem; e.bgWG.Done() }()
		e.drive(bctx, jobs)
	}()
}

// Close 等待后台推进结束；ctx 到期后取消它们：正在执行的节点被打断后放回队列（不计重试次数），由 tick 接手。
// 之后不应再调用引擎。
func (e *Engine) Close(ctx context.Context) error {
	done := make(chan struct{})
	go func() { e.bgWG.Wait(); close(done) }()
	select {
	case <-done:
		e.bgStop()
		return nil
	case <-ctx.Done():
		e.bgStop()
		<-done
		return ctx.Err()
	}
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

// TickOptions 控制一次 tick 处理多少到期 job；并发与每批的预算见 Options。
type TickOptions struct {
	BatchSize  int // 每批读取的到期 job 数，默认 100
	MaxBatches int // 一次 tick 最多处理的批数，默认 20；剩下的留给下一次 tick
}

// ProcessDue 由 xxl-job 的 tick 调用：执行全部到期的 job（定时器、超时、退避后的重试、租约过期的 job）。
// 每批交给一次 drive，批内的 job 与它们新就绪的后续共用预算与并发上限；超出预算的后续会被下一批读到。
// 返回处理的 job 数。
func (e *Engine) ProcessDue(ctx context.Context, opts TickOptions) (int, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.MaxBatches <= 0 {
		opts.MaxBatches = 20
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
		n := e.drive(ctx, jobs)
		total += n
		if n == 0 {
			break // 读到的都已被别人处理或持有
		}
	}
	return total, nil
}
