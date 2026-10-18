package tasktypeservicelogic

import (
	"context"
	"regexp"
	"strings"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateTaskTypeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateTaskTypeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTaskTypeLogic {
	return &CreateTaskTypeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 新建任务类型并创建草稿
func (l *CreateTaskTypeLogic) CreateTaskType(in *pb.CreateTaskTypeReq) (*pb.CreateTaskTypeResp, error) {
	actor, err := identity.Require(l.ctx)
	if err != nil {
		return nil, err
	}
	if !keyPattern.MatchString(in.Key) {
		return nil, status.Error(codes.InvalidArgument, "key must match ^[a-z][a-z0-9_]{1,63}$")
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Namespace) == "" {
		return nil, status.Error(codes.InvalidArgument, "name and namespace are required")
	}
	d, err := l.svcCtx.TaskTypes.Create(l.ctx, in.Key, strings.TrimSpace(in.Name), strings.TrimSpace(in.Namespace), in.Description, actor)
	if err != nil {
		return nil, draftError(err)
	}
	return &pb.CreateTaskTypeResp{Key: in.Key, Draft: toDraft(d)}, nil
}

// keyPattern 与 task_type.schema.json 中 key 的格式一致。
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
