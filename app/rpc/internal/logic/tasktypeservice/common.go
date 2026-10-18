package tasktypeservicelogic

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

// 草稿、变更集与组件库尚未实现：任务类型的只读接口直接读已发布的 wf_plan（由 registry.PublishDirect 写入）。
// 接入配置中心后改为读 cfg_version 与发布清单。

func plans(s *svc.ServiceContext) model.WfPlanModel { return model.NewWfPlanModel(s.Conn) }

// summary 用某个版本的定义生成摘要。
func summary(row *model.WfPlan) (*pb.TaskTypeSummary, error) {
	t, err := spec.Parse([]byte(row.Dsl))
	if err != nil {
		return nil, fmt.Errorf("task type %s@%d: %w", row.PlanId, row.Version, err)
	}
	return &pb.TaskTypeSummary{
		Key: row.PlanId, Name: t.Name, Namespace: t.Namespace, LatestVersion: int32(row.Version),
		Status: "active", UpdatedAt: row.PublishedAt.UnixMilli(),
	}, nil
}

func versionInfo(row *model.WfPlan) *pb.VersionInfo {
	return &pb.VersionInfo{
		Version: int32(row.Version), ContentHash: row.Checksum, State: "published",
		PublishedBy: row.PublishedBy, PublishedAt: row.PublishedAt.UnixMilli(),
	}
}

// findPlan 取某个版本；version 为 0 取最新。
func findPlan(ctx context.Context, s *svc.ServiceContext, key string, version int32) (*model.WfPlan, error) {
	m := plans(s)
	if version == 0 {
		max, err := m.MaxVersion(ctx, key)
		if err != nil {
			return nil, err
		}
		if max == 0 {
			return nil, status.Errorf(codes.NotFound, "task type %q has no published version", key)
		}
		version = int32(max)
	}
	row, err := m.FindOneByPlanIdVersion(ctx, key, int64(version))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "task type %s@%d not found", key, version)
	}
	return row, err
}

func toIssues(in []compiler.Issue) []*pb.Issue {
	out := make([]*pb.Issue, 0, len(in))
	for _, i := range in {
		out = append(out, &pb.Issue{Code: string(i.Code), Severity: string(i.Severity), Path: i.Path, NodeId: i.NodeID, EdgeId: i.EdgeID, Message: i.Message})
	}
	return out
}

func toDraft(d *registry.TaskTypeDraft) *pb.Draft {
	return &pb.Draft{BaseVersion: int32(d.BaseVersion), Revision: d.Revision, BodyJson: d.Body, UpdatedBy: d.UpdatedBy,
		UpdatedAt: d.UpdatedAt.UnixMilli(), ContentHash: d.ContentHash}
}

// draftError 把草稿操作的错误映射为 gRPC 状态码。
func draftError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, registry.ErrNoTaskType), errors.Is(err, registry.ErrNoDraft):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, registry.ErrExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, registry.ErrConflict):
		return status.Error(codes.Aborted, err.Error()) // 前端据此提示「草稿已被修改」
	case errors.Is(err, registry.ErrKeyChanged), errors.Is(err, registry.ErrDraftInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return err
	}
}
