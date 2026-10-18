// Package identity 读取调用人身份。身份由公司 gateway 认证后写入 gRPC metadata，
// 本地开发由 cmd/devgw 按 X-Dev-User 请求头写入同一个 key；请求消息里不带身份字段。
package identity

import (
	"context"

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
