# worker

引擎执行进程，与 `app/rpc`（对外 gRPC 接口，含 Lark 与审批回调）分开部署。

```bash
go run ./app/worker -f app/worker/etc/worker.yaml
```

进程内没有工作池，也不轮询数据库。流程只在事件到来时推进：

- **事件**：来源扫描到新数据启动 run，扫描到状态变化（`close_when`）取消 run；人工操作经 `Signal` 进入。
  事件处理完后在同一个调用里继续执行新就绪的节点，直到停在等待点（人工、定时器、重试退避）或结束。
- **ticker**（`wf-ticker`）是唯一的时钟：触发到期的 cron 来源（`wf_schedule`），并执行到期的 `wf_job`
  （定时器、超时、退避后的重试、进程崩溃后租约过期的 job）。`Ticker.Mode: local` 用进程内定时器；
  `xxljob` 向调度中心注册执行器 `AppName`，需要在调度中心新建 BEAN 任务，JobHandler 为 `wf-ticker`，每 10~30 秒一次。
  定时器的精度就是 tick 的周期。
- **配置**：`DataSource` 是平台库，`BizDataSource` 是业务只读库（内置 skill 使用），`RelationsFile` 是本地关系表。
  组件的组装见 `internal/bootstrap`。
