package config

import (
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
)

type Config struct {
	zrpc.RpcServerConf
	Workbench bootstrap.Config // 引擎、任务中心与动作实现的组装配置，与 app/worker 相同
	// AllowDirectPublish 允许 PublishTaskTypeDraft 跳过变更集与审批直接发布，只在开发环境开启。
	AllowDirectPublish bool `json:",optional"`
}
