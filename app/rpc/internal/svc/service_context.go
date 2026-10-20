package svc

import (
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/config"
	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
)

type ServiceContext struct {
	Config config.Config
	*bootstrap.Services
}

func NewServiceContext(c config.Config) *ServiceContext {
	s, err := bootstrap.Build(c.Workbench, nil)
	if err != nil {
		panic(err)
	}
	return &ServiceContext{Config: c, Services: s}
}
