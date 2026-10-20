// Package ticker 是 xxl-job 驱动的外部时钟。
//
// 只在 xxl-job 注册一个调度任务 wb-ticker（每分钟），每次触发做三件事：
//  1. 执行到期的 SQL 数据源（新 key 建任务、消失的 key 关闭任务）；
//  2. 推迟到期的任务回到待处理并重新通知；
//  3. 重试投递失败的 Lark 通知。
//
// Tick 必须幂等：重复触发无副作用，漏触发由下一次补上。平时没有任何轮询与常驻工作池。
package ticker

import (
	"context"
	"errors"
	"fmt"
	"time"

	xxl "github.com/xxl-job/xxl-job-executor-go"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/notify"
	"github.com/alex2025-sh/ai-workbench/internal/source"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

// JobName 是在 xxl-job 中注册的任务名。
const JobName = "wb-ticker"

// Ticker 处理一次时钟触发。
type Ticker struct {
	Sources *source.Runner
	Tasks   *task.Store
	Notify  *notify.Dispatcher
}

// Tick 执行一次。
func (t *Ticker) Tick(ctx context.Context) error {
	var errs []error
	for _, st := range t.Sources.RunDue(ctx) {
		if st.Err != nil {
			errs = append(errs, fmt.Errorf("source %s: %w", st.TaskType, st.Err))
		}
		if st.Created+st.Closed > 0 {
			logx.WithContext(ctx).Infof("ticker: source %s rows=%d created=%d closed=%d", st.TaskType, st.Rows, st.Created, st.Closed)
		}
	}
	if n, err := t.Tasks.WakeDue(ctx, 500); err != nil {
		errs = append(errs, fmt.Errorf("wake: %w", err))
	} else if n > 0 {
		logx.WithContext(ctx).Infof("ticker: woke %d snoozed tasks", n)
	}
	if _, err := t.Notify.RetryDue(ctx, 500); err != nil {
		errs = append(errs, fmt.Errorf("notify: %w", err))
	}
	return errors.Join(errs...)
}

// RunLocal 用进程内定时器代替 xxl-job 调度中心，每 interval 触发一次，只用于本地开发。
func RunLocal(ctx context.Context, t *Ticker, interval time.Duration) {
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		if err := t.Tick(ctx); err != nil {
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

// RunXxlJob 向 xxl-job 调度中心注册执行器与 wb-ticker 任务；阻塞直到 ctx 结束。
// 在调度中心为 AppName 新建任务（运行模式 BEAN，JobHandler 为 wb-ticker，每分钟）。
func RunXxlJob(ctx context.Context, t *Ticker, c XxlJobConfig) error {
	exec := xxl.NewExecutor(
		xxl.ServerAddr(c.AdminAddr),
		xxl.AccessToken(c.AccessToken),
		xxl.ExecutorPort(c.Port),
		xxl.RegistryKey(c.AppName),
	)
	exec.Init()
	exec.RegTask(JobName, func(cctx context.Context, _ *xxl.RunReq) string {
		if err := t.Tick(cctx); err != nil {
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
