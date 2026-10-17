package ticker

import (
	"context"
	"fmt"
	"time"

	xxl "github.com/xxl-job/xxl-job-executor-go"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/ingest"
)

// Ticker 实现 Handler，是引擎唯一的时钟：
//  1. 触发到期的 cron 来源（wf_schedule），新数据创建任务并启动 run，run 启动后就地推进；
//  2. 执行到期的 job：定时器、超时、退避后的重试，以及进程崩溃后租约过期的 job。
//
// 平时没有任何轮询：推进只由来源、人工操作、外部状态变化和这里的 tick 触发。
type Ticker struct {
	Cron    *ingest.CronRunner
	Engine  *engine.Engine
	Options engine.TickOptions
}

// Tick 实现 Handler。
func (t *Ticker) Tick(ctx context.Context, _ Shard) error {
	var firstErr error
	for _, r := range t.Cron.FireDue(ctx) {
		if r.Err != nil && firstErr == nil {
			firstErr = fmt.Errorf("ticker: %s: %w", r.Schedule, r.Err)
		}
		if r.Scanned > 0 {
			logx.WithContext(ctx).Infof("ticker: %s scanned=%d filtered=%d invalid=%d created=%d closed=%d",
				r.Schedule, r.Scanned, r.Filtered, r.Invalid, r.Created, r.Closed)
		}
	}
	n, err := t.Engine.ProcessDue(ctx, t.Options)
	if n > 0 {
		logx.WithContext(ctx).Infof("ticker: processed %d due jobs", n)
	}
	if err != nil && firstErr == nil {
		firstErr = fmt.Errorf("ticker: process due jobs: %w", err)
	}
	return firstErr
}

// RunLocal 用进程内定时器代替 xxl-job 调度中心，每 interval 触发一次 Tick，只用于本地开发。
func RunLocal(ctx context.Context, h Handler, interval time.Duration) {
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		if err := h.Tick(ctx, Shard{Index: 0, Total: 1}); err != nil {
			logx.WithContext(ctx).Errorf("%v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
	}
}

// XxlJobConfig 是 xxl-job 执行器的配置。
type XxlJobConfig struct {
	AdminAddr   string // 如 http://127.0.0.1:8080/xxl-job-admin
	AccessToken string `json:",optional"`
	AppName     string `json:",default=ai-workbench"` // 执行器 AppName，需要在调度中心登记
	Port        string `json:",default=9999"`
}

// RunXxlJob 向 xxl-job 调度中心注册执行器，并注册 wf-ticker 任务；阻塞直到 ctx 结束。
// 在调度中心为 AppName 新建任务 JobName（运行模式 BEAN，JobHandler 为 wf-ticker，每 10~30 秒）。
func RunXxlJob(ctx context.Context, h Handler, c XxlJobConfig) error {
	exec := xxl.NewExecutor(
		xxl.ServerAddr(c.AdminAddr),
		xxl.AccessToken(c.AccessToken),
		xxl.ExecutorPort(c.Port),
		xxl.RegistryKey(c.AppName),
	)
	exec.Init()
	exec.RegTask(JobName, func(cctx context.Context, req *xxl.RunReq) string {
		err := h.Tick(cctx, Shard{Index: int(req.BroadcastIndex), Total: int(req.BroadcastTotal)})
		if err != nil {
			return err.Error()
		}
		return "ok"
	})
	errc := make(chan error, 1)
	go func() { errc <- exec.Run() }()
	select {
	case <-ctx.Done():
		exec.Stop()
		return nil
	case err := <-errc:
		return err
	}
}
