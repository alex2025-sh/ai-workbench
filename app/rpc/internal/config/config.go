package config

import (
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
)

type Config struct {
	zrpc.RpcServerConf
	Workbench bootstrap.Config // 与 app/worker 相同的组装配置
}
