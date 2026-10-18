package svc

import (
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/config"
	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
)

type ServiceContext struct {
	Config config.Config
	*bootstrap.Services
	TaskTypes *registry.TaskTypes // 任务类型的草稿与开发环境发布
}

func NewServiceContext(c config.Config) *ServiceContext {
	s, err := bootstrap.Build(c.Workbench, nil)
	if err != nil {
		panic(err)
	}
	return &ServiceContext{Config: c, Services: s, TaskTypes: registry.NewTaskTypes(s.Conn)}
}
