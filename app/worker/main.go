// Command worker 是引擎进程：注册 xxl-job 执行器，由 wf-ticker 的每次 tick 触发 cron 来源、唤醒到期的定时器与超时。
// 进程内没有轮询；人工操作与外部状态变化经 gRPC 接口（RunService.Signal / CancelRun）进入，同样就地推进。
//
//	go run ./app/worker -f app/worker/etc/worker.yaml
package main

import (
	"context"
	"flag"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/ticker"
)

// Config 是 worker 的配置文件结构。
type Config struct {
	bootstrap.Config
	Log    logx.LogConf `json:",optional"`
	Ticker struct {
		Mode        string              `json:",default=local,options=local|xxljob"`
		Interval    time.Duration       `json:",default=15s"` // local 模式的触发间隔
		Parallelism int                 `json:",default=4"`   // 一次 tick 内并行执行的到期 job 数
		XxlJob      ticker.XxlJobConfig `json:",optional"`
	} `json:",optional"`
}

var configFile = flag.String("f", "app/worker/etc/worker.yaml", "config file")

func main() {
	flag.Parse()
	var c Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)

	svc, err := bootstrap.Build(c.Config, nil)
	if err != nil {
		logx.Must(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	svc.Ticker.Options = engine.TickOptions{Parallelism: c.Ticker.Parallelism}

	switch c.Ticker.Mode {
	case "xxljob":
		logx.Infof("worker: ticker via xxl-job %s (app %s)", c.Ticker.XxlJob.AdminAddr, c.Ticker.XxlJob.AppName)
		logx.Must(ticker.RunXxlJob(ctx, svc.Ticker, c.Ticker.XxlJob))
	default:
		logx.Infof("worker: local ticker every %s", c.Ticker.Interval)
		ticker.RunLocal(ctx, svc.Ticker, c.Ticker.Interval)
	}
	// 退出前等待后台推进结束；超时则取消，已认领的 job 立即释放，由其他实例的 tick 接手。
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.Engine.Close(sctx); err != nil {
		logx.Errorf("worker: engine close: %v", err)
	}
}
