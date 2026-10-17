// Command worker 是引擎执行进程：Worker 池执行 job，ticker（本地定时器或 xxl-job）触发 cron 来源。
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
	Worker struct {
		Concurrency  int           `json:",default=8"`
		PollInterval time.Duration `json:",default=1s"`
	} `json:",optional"`
	Ticker struct {
		Mode     string              `json:",default=local,options=local|xxljob"`
		Interval time.Duration       `json:",default=15s"` // local 模式的触发间隔
		XxlJob   ticker.XxlJobConfig `json:",optional"`
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

	w := engine.NewWorker(svc.Engine, engine.WorkerOptions{Concurrency: c.Worker.Concurrency, PollInterval: c.Worker.PollInterval})
	go w.Run(ctx)

	switch c.Ticker.Mode {
	case "xxljob":
		logx.Infof("worker: ticker via xxl-job %s (app %s)", c.Ticker.XxlJob.AdminAddr, c.Ticker.XxlJob.AppName)
		logx.Must(ticker.RunXxlJob(ctx, svc.Ticker, c.Ticker.XxlJob))
	default:
		logx.Infof("worker: local ticker every %s", c.Ticker.Interval)
		ticker.RunLocal(ctx, svc.Ticker, c.Ticker.Interval)
	}
}
