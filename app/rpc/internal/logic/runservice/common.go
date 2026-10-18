package runservicelogic

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/cursor"
)

// queryError 把常见的查询错误映射为 gRPC 状态：记录不存在为 NotFound，游标无法解析为 InvalidArgument。
func queryError(err error, what string) error {
	switch {
	case errors.Is(err, model.ErrNotFound):
		return status.Errorf(codes.NotFound, "%s not found", what)
	case errors.Is(err, cursor.ErrBad):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return err
	}
}
