package taskservicelogic

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
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
