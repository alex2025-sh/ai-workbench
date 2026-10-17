package ticker

import (
	"context"
	"fmt"
	"time"

	xxl "github.com/xxl-job/xxl-job-executor-go"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/ingest"
)

// Ticker 实现 Handler：触发到期的 cron 来源。
//
// 到期的 timer / timeout job 与租约过期的 job 由 Worker 的轮询认领（CAS 保证不重复），
// 因此 Tick 只负责 wf_schedule；巡检告警在运维阶段补上。
type Ticker struct {
	Cron *ingest.CronRunner
}

// Tick 实现 Handler。
func (t *Ticker) Tick(ctx context.Context, _ Shard) error {
	for _, r := range t.Cron.FireDue(ctx) {
		if r.Err != nil {
			return fmt.Errorf("ticker: %s: %w", r.Schedule, r.Err)
		}
		if r.Scanned > 0 {
			logx.WithContext(ctx).Infof("ticker: %s scanned=%d filtered=%d invalid=%d created=%d", r.Schedule, r.Scanned, r.Filtered, r.Invalid, r.Created)
		}
	}
	return nil
}

// RunLocal 用进程内定时器代替 xxl-job，每 interval 触发一次，用于本地开发。
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
