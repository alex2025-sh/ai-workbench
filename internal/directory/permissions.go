package directory

import (
	"context"
	"fmt"
	"os"
	"slices"

	"go.yaml.in/yaml/v3"
)

// StaticPermissions 是内存中的接口级权限表：用户 → 权限。用于本地开发与测试。
type StaticPermissions map[string][]string

// LoadPermissions 从 YAML 文件加载权限，格式见 deploy/mock/permissions.yaml：
//
//	ops@example.com: [task.admin]
func LoadPermissions(path string) (StaticPermissions, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p StaticPermissions
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("directory: parse %s: %w", path, err)
	}
	return p, nil
}

// Allowed 实现 Authorizer。
func (p StaticPermissions) Allowed(_ context.Context, userID, permission string) (bool, error) {
	return slices.Contains(p[userID], permission), nil
}
