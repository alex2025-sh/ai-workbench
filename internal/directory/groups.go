package directory

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"

	"go.yaml.in/yaml/v3"
)

// Groups 查询用户组成员：组任务的认领、决策鉴权与「可认领」列表都依赖它。
// 线上来自 BSP 的用户组；本地开发读 deploy/mock/groups.yaml。
type Groups interface {
	IsMember(ctx context.Context, group, userID string) (bool, error)
	GroupsOf(ctx context.Context, userID string) ([]string, error)
}

// StaticGroups 是内存中的用户组表：组 → 成员。用于本地开发与测试。
type StaticGroups map[string][]string

// LoadGroups 从 YAML 文件加载用户组，格式见 deploy/mock/groups.yaml：
//
//	vip_cs_leads: [lead@example.com]
func LoadGroups(path string) (StaticGroups, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g StaticGroups
	if err := yaml.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("directory: parse %s: %w", path, err)
	}
	return g, nil
}

// IsMember 实现 Groups。
func (g StaticGroups) IsMember(_ context.Context, group, userID string) (bool, error) {
	return slices.Contains(g[group], userID), nil
}

// GroupsOf 实现 Groups，按组名排序。
func (g StaticGroups) GroupsOf(_ context.Context, userID string) ([]string, error) {
	var out []string
	for group, members := range g {
		if slices.Contains(members, userID) {
			out = append(out, group)
		}
	}
	sort.Strings(out)
	return out, nil
}
