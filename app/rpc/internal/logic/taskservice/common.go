package taskservicelogic

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/taskview"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/cursor"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

// loadTask 读取任务；不存在时返回 NotFound。
func loadTask(ctx context.Context, svcCtx *svc.ServiceContext, id int64) (*task.Task, error) {
	t, err := svcCtx.Tasks.Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "task %d not found", id)
	}
	return t, err
}

// listQuery 把列表请求的公共条件转为任务中心的查询。
func listQuery(statuses []pb.TaskStatus, priorities []pb.TaskPriority, taskType string, page *pb.PageReq) (task.ListQuery, error) {
	q := task.ListQuery{TaskType: taskType, Cursor: page.GetCursor(), PageSize: int(page.GetPageSize())}
	for _, v := range statuses {
		s, ok := task.StatusFromProto(int32(v))
		if !ok {
			return q, status.Errorf(codes.InvalidArgument, "bad status %v", v)
		}
		q.Statuses = append(q.Statuses, s)
	}
	for _, v := range priorities {
		p, ok := task.PriorityFromProto(int32(v))
		if !ok {
			return q, status.Errorf(codes.InvalidArgument, "bad priority %v", v)
		}
		q.Priorities = append(q.Priorities, p)
	}
	return q, nil
}

// queryError 把游标错误映射为 InvalidArgument。
func queryError(err error) error {
	if errors.Is(err, cursor.ErrBad) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return err
}

// taskError 把任务中心的错误映射为 gRPC 状态码。
func taskError(err error) error {
	var bad task.ErrInvalidTransition
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrNotFound):
		return status.Error(codes.NotFound, "task not found")
	case errors.Is(err, task.ErrNotClaimable), errors.As(err, &bad):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, task.ErrNotAssignee):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, task.ErrReasonRequired):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return err
	}
}

// requireOwnerOrAdmin 允许任务的处理人，或有 task.admin 权限的人（如组长、运营）。
func requireOwnerOrAdmin(ctx context.Context, svcCtx *svc.ServiceContext, t *task.Task, actor string) error {
	if t.AssigneeID != "" && t.AssigneeID == actor {
		return nil
	}
	return identity.RequirePermission(ctx, svcCtx.Authz, actor, identity.PermTaskAdmin)
}

// view 返回操作后的任务视图。
func view(ctx context.Context, svcCtx *svc.ServiceContext, t *task.Task) *pb.TaskView {
	return taskview.Builder{S: svcCtx.Services}.View(ctx, t)
}
