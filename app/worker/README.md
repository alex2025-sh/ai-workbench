# worker

引擎执行进程，与 `app/api` 分开部署：

- **Worker 池**：CAS 认领 `wf_job`，在事务外调用节点执行器，把结果交给推进器提交。
- **推进器**：唯一修改节点状态的组件，见 `internal/flow/engine`。
- **xxl-job 执行器**：注册 `wf-ticker` 调度任务，入口见 `internal/ticker`。

服务入口（`main`、go-zero 配置加载、xxl-job 执行器注册）在引擎阶段一实现时补上，
保持只依赖 TiDB 与 xxl-job。
