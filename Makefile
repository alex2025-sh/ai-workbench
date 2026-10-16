GOCTL ?= goctl
STYLE ?= go_zero

.PHONY: all gen gen-api gen-model tidy build vet test fmt check

all: check

## 代码生成（需要本地安装 goctl：go install github.com/zeromicro/go-zero/tools/goctl@latest）
gen: gen-api gen-model tidy

# 根据 app/api/workbench.api 生成 handler / logic / svc / types / etc 配置
gen-api:
	cd app/api && $(GOCTL) api go --api workbench.api --dir . --style $(STYLE)

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
