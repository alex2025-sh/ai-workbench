GOCTL ?= goctl
STYLE ?= go_zero

.PHONY: all gen gen-rpc gen-api gen-model tidy build vet test fmt check

all: check

## 代码生成
# 需要本地安装 goctl、protoc、protoc-gen-go、protoc-gen-go-grpc：
#   go install github.com/zeromicro/go-zero/tools/goctl@latest
#   goctl env check --install --verbose --force
gen: gen-rpc gen-api gen-model tidy

# 对外业务接口：根据 app/rpc/workbench.proto 生成 zrpc 服务（多 service，需 -m）
gen-rpc:
	cd app/rpc && $(GOCTL) rpc protoc workbench.proto --go_out=. --go-grpc_out=. --zrpc_out=. -m --style $(STYLE)

# 第三方 HTTP 回调：根据 app/callback/callback.api 生成（仅 Lark 卡片回调）
gen-api:
	cd app/callback && $(GOCTL) api go --api callback.api --dir . --style $(STYLE)

# 根据 deploy/sql/*.sql 生成 internal/model 下的 *_model.go 与 *_model_gen.go
gen-model:
	$(GOCTL) model mysql ddl --src "deploy/sql/*.sql" --dir internal/model --style $(STYLE)

tidy:
	go mod tidy

## 构建与检查
build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

fmt:
	gofmt -s -w .

check: vet test build
