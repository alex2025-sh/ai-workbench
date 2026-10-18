package runservicelogic

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
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

// interveneError 把运行干预的错误映射为 gRPC 状态码。
func interveneError(err error, runID int64) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrNotFound):
		return status.Errorf(codes.NotFound, "run %d not found", runID)
	case errors.Is(err, engine.ErrUnknownNode), errors.Is(err, engine.ErrInvalidPort):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, engine.ErrNotPaused), errors.Is(err, engine.ErrRunEnded),
		errors.Is(err, engine.ErrNothingToRetry), errors.Is(err, engine.ErrNodeConcluded):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return err
	}
}

// adminActor 返回有 run.admin 权限的调用人；原因必填。
func adminActor(ctx context.Context, svcCtx *svc.ServiceContext, reason string) (string, error) {
	actor, err := identity.Require(ctx)
	if err != nil {
		return "", err
	}
	if err := identity.RequirePermission(ctx, svcCtx.Authz, actor, identity.PermRunAdmin); err != nil {
		return "", err
	}
	if strings.TrimSpace(reason) == "" {
		return "", status.Error(codes.InvalidArgument, "reason is required")
	}
	return actor, nil
}
