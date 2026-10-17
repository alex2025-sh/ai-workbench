GOCTL ?= goctl
STYLE ?= go_zero

.PHONY: all gen gen-rpc gen-model tidy build vet test fmt check

all: check

## 代码生成
# 需要 protoc（含 google/protobuf 标准 proto）、protoc-gen-go、protoc-gen-go-grpc 与 goctl：
#   go install github.com/zeromicro/go-zero/tools/goctl@latest
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
gen: gen-rpc gen-model tidy

# 对外接口：根据 app/rpc/workbench.proto 生成 zrpc 服务（多 service，需 -m）
gen-rpc:
	cd app/rpc && $(GOCTL) rpc protoc workbench.proto -I . \
		--go_out=. --go-grpc_out=. --zrpc_out=. -m --style $(STYLE)

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
