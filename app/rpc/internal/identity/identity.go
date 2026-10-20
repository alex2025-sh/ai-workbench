// Package identity 读取调用人身份。身份由公司 gateway 认证后写入 gRPC metadata，
// 本地开发由 cmd/devgw 按 X-Dev-User 请求头写入同一个 key；请求消息里不带身份字段。
package identity

import (
	"context"

	"github.com/alex2025-sh/ai-workbench/internal/directory"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MetadataKey 是调用人 ID 所在的 metadata key。接入 gateway 时与其约定对齐。
const MetadataKey = "x-user-id"

// UserID 返回调用人 ID；没有身份时为空。
func UserID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if v := md.Get(MetadataKey); len(v) > 0 {
		return v[0]
	}
	return ""
}

// Require 返回调用人 ID；没有身份时返回 Unauthenticated。
func Require(ctx context.Context) (string, error) {
	if u := UserID(ctx); u != "" {
		return u, nil
	}
	return "", status.Error(codes.Unauthenticated, "missing caller identity")
}

// 接口级权限。线上在 BSP 权限平台按接口配置，本地见 deploy/mock/permissions.yaml。
const (
	PermTaskAdmin = "task.admin"        // 查看与转派任意任务、调整优先级
	PermPublish   = "task_type.publish" // 发布任务类型草稿
	PermTeamAdmin = "team.admin"        // 维护团队与成员
)

// Allowed 判断调用人是否有某个接口级权限。
func Allowed(ctx context.Context, authz directory.Authorizer, user, permission string) (bool, error) {
	if authz == nil {
		return false, nil
	}
	return authz.Allowed(ctx, user, permission)
}

// RequirePermission 在调用人没有权限时返回 PermissionDenied。
func RequirePermission(ctx context.Context, authz directory.Authorizer, user, permission string) error {
	ok, err := Allowed(ctx, authz, user, permission)
	if err != nil {
		return err
	}
	if !ok {
		return status.Errorf(codes.PermissionDenied, "permission %s required", permission)
	}
	return nil
}
