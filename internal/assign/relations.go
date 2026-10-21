package assign

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

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
		if len(m) == 0 {
			r.Set(rel, "", nil...) // 只声明关系名
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
	if subject != "" {
		r.data[relation][subject] = who
	}
}

// Lookup 实现 Relations。
func (r *StaticRelations) Lookup(_ context.Context, relation, subject string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data[relation][subject], nil
}

// Names 实现 Relations，按名字排序。
func (r *StaticRelations) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.data))
	for k := range r.data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
