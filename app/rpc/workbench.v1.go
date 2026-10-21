package main

import (
	"flag"
	"fmt"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/config"
	catalogserviceServer "github.com/alex2025-sh/ai-workbench/app/rpc/internal/server/catalogservice"
	ingestserviceServer "github.com/alex2025-sh/ai-workbench/app/rpc/internal/server/ingestservice"
	taskserviceServer "github.com/alex2025-sh/ai-workbench/app/rpc/internal/server/taskservice"
	tasktypeserviceServer "github.com/alex2025-sh/ai-workbench/app/rpc/internal/server/tasktypeservice"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// 在仓库根目录运行：go run ./app/rpc -f app/rpc/etc/workbench.yaml
// 在仓库根目录运行：go run ./app/rpc -f app/rpc/etc/workbench.yaml
var configFile = flag.String("f", "app/rpc/etc/workbench.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterTaskTypeServiceServer(grpcServer, tasktypeserviceServer.NewTaskTypeServiceServer(ctx))
		pb.RegisterTaskServiceServer(grpcServer, taskserviceServer.NewTaskServiceServer(ctx))
		pb.RegisterCatalogServiceServer(grpcServer, catalogserviceServer.NewCatalogServiceServer(ctx))
		pb.RegisterIngestServiceServer(grpcServer, ingestserviceServer.NewIngestServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
