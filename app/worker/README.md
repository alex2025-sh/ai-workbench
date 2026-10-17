# worker

引擎执行进程，与 `app/rpc`（对外 gRPC 接口，含 Lark 与审批回调）分开部署。

```bash
go run ./app/worker -f app/worker/etc/worker.yaml
```

- **Worker 池**：每秒轮询到期的 `wf_job`，CAS 认领后在事务外执行节点，再交给推进器在锁 `wf_run` 行的事务内提交；
  提交后新就绪的 job 直接进入本进程的执行队列。租约过期的 job 会被任意 worker 重新认领。
- **ticker**：触发到期的 cron 来源（`wf_schedule`）。`Ticker.Mode: local` 用进程内定时器；
  `xxljob` 向调度中心注册执行器 `AppName`，需要在调度中心新建 BEAN 任务，JobHandler 为 `wf-ticker`，每 10~30 秒一次。
- **配置**：`DataSource` 是平台库，`BizDataSource` 是业务只读库（内置 skill 使用），`RelationsFile` 是本地关系表。
  组件的组装见 `internal/bootstrap`。
