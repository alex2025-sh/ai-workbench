// Package assign 求值节点的分配器：个人（业务关系）或团队（认领 / 分配模式）。
package assign

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// Target 是任务进入节点时的去向：具体的人，或团队队列。
type Target struct {
	Assignee string // 非空：直接分配给这个人
	Team     string // Assignee 为空时：进入该团队的队列
	Mode     string // 团队模式：claim / assign
}

// Relations 提供业务关系，如 VIP uid → 专属 RM。线上来自关系 API，本地读 deploy/mock/vip_rm.yaml。
type Relations interface {
	Lookup(ctx context.Context, relation, subject string) ([]string, error)
}

// Resolve 求值节点的分配器。个人关系查不到人时进入兜底团队的认领队列。
// 只做读取，没有副作用，可以在事务外调用。
func Resolve(ctx context.Context, rel Relations, n tasktype.Node, vals tasktype.Values) (Target, error) {
	a := n.Assignee
	switch a.Kind {
	case tasktype.AssigneeTeam:
		return Target{Team: a.Team, Mode: a.Mode}, nil
	case tasktype.AssigneeRelation:
		subject := ""
		if v, ok := vals[a.Field]; ok && v != nil {
			subject = fmt.Sprint(v)
		}
		if subject != "" && rel != nil {
			who, err := rel.Lookup(ctx, a.Relation, subject)
			if err != nil {
				return Target{}, fmt.Errorf("assign: relation %s(%s): %w", a.Relation, subject, err)
			}
			if len(who) > 0 && who[0] != "" {
				return Target{Assignee: who[0]}, nil
			}
		}
		return Target{Team: a.FallbackTeam, Mode: tasktype.ModeClaim}, nil
	default:
		return Target{}, fmt.Errorf("assign: unknown assignee kind %q", a.Kind)
	}
}

// StaticRelations 是内存中的业务关系表：关系名 → 主体 → 候选人。用于本地开发与测试。
type StaticRelations struct {
	mu   sync.RWMutex
	data map[string]map[string][]string
}

// LoadRelations 从 YAML 文件加载关系表，格式见 deploy/mock/vip_rm.yaml。
func LoadRelations(path string) (*StaticRelations, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]map[string]string
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("assign: parse %s: %w", path, err)
	}
	r := &StaticRelations{}
	for rel, m := range doc {
		for subject, who := range m {
			r.Set(rel, subject, strings.Split(who, ",")...)
		}
	}
	return r, nil
}

// Set 设置一条关系。
func (r *StaticRelations) Set(relation, subject string, who ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.data == nil {
		r.data = map[string]map[string][]string{}
	}
	if r.data[relation] == nil {
		r.data[relation] = map[string][]string{}
	}
	r.data[relation][subject] = who
}

// Lookup 实现 Relations。
func (r *StaticRelations) Lookup(_ context.Context, relation, subject string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data[relation][subject], nil
}

// Names 返回已知的关系名（配置页的下拉选项）。
func (r *StaticRelations) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for k := range r.data {
		out = append(out, k)
	}
	return out
}
