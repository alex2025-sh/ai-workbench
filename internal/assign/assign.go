// Package assign 求值节点的分配器：任务进入节点时交给谁。
//
// 一期支持三种分配器（tasktype.Assignee.Kind）：
//   - relation：按业务关系找到一个人（如 VIP → RM），查不到时交给配置的兜底人；
//   - pool：从配置的一批人中按策略选一个（目前只有 random）；
//   - claim：不指定人，任务进入「待认领」并通知到 Lark 群，由打开链接的人自己认领（Target.Assignee 为空）。
//
// 扩展点：
//   - 新的分配方式实现 Resolver 并注册到 Service（如按组织架构、按排班表）；
//   - 新的业务关系实现 Relations（线上为关系服务，本地读 YAML）；
//   - 新的选择策略实现 Picker 并注册（如轮询、按负载）。
package assign

import (
	"context"
	"fmt"

	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// Target 是分配结果：处理人，以及是怎么分到的（写进任务历史）。
type Target struct {
	Assignee string
	By       string // relation / fallback / random / claim ...
}

// Resolver 是一种分配方式。实现只读取、没有副作用，在事务外调用。
type Resolver interface {
	// Kind 是配置中的 assignee.kind。
	Kind() string
	// Resolve 返回处理人；vals 是任务的业务字段。
	Resolve(ctx context.Context, a tasktype.Assignee, vals tasktype.Values) (Target, error)
}

// Relations 提供业务关系，如 VIP uid → 专属 RM。线上为关系服务，本地读 deploy/mock/vip_rm.yaml。
type Relations interface {
	Lookup(ctx context.Context, relation, subject string) ([]string, error)
	// Names 返回可用的关系名（配置页的下拉选项与发布前的引用检查）。
	Names() []string
}

// Picker 从候选人中选一个。key 是任务的去重键，按负载等策略可能用到。
type Picker interface {
	Strategy() string
	Pick(ctx context.Context, users []string, key string) (string, error)
}

// Service 按 assignee.kind 分发到对应的 Resolver。
type Service struct {
	resolvers map[string]Resolver
}

// New 创建 Service 并注册内置的两种分配方式。
func New(rel Relations, pickers ...Picker) *Service {
	s := &Service{resolvers: map[string]Resolver{}}
	s.Register(RelationResolver{Relations: rel})
	pool := PoolResolver{Pickers: map[string]Picker{}}
	for _, p := range append([]Picker{Random{}}, pickers...) {
		pool.Pickers[p.Strategy()] = p
	}
	s.Register(pool)
	s.Register(ClaimResolver{})
	return s
}

// Register 注册（或替换）一种分配方式。
func (s *Service) Register(r Resolver) { s.resolvers[r.Kind()] = r }

// Resolve 求值节点的分配器。
func (s *Service) Resolve(ctx context.Context, n tasktype.Node, vals tasktype.Values) (Target, error) {
	r, ok := s.resolvers[n.Assignee.Kind]
	if !ok {
		return Target{}, fmt.Errorf("assign: unknown assignee kind %q", n.Assignee.Kind)
	}
	t, err := r.Resolve(ctx, n.Assignee, vals)
	if err != nil {
		return Target{}, err
	}
	if t.Assignee == "" && n.Assignee.Kind != tasktype.AssigneeClaim {
		return Target{}, fmt.Errorf("assign: node %s resolved to nobody", n.ID)
	}
	return t, nil
}

// RelationResolver：按业务关系找到一个人，查不到时交给兜底人。关系服务出错时返回错误（数据源下一轮重试），不走兜底。
type RelationResolver struct {
	Relations Relations
}

// Kind 实现 Resolver。
func (RelationResolver) Kind() string { return tasktype.AssigneeRelation }

// Resolve 实现 Resolver。
func (r RelationResolver) Resolve(ctx context.Context, a tasktype.Assignee, vals tasktype.Values) (Target, error) {
	subject := ""
	if v, ok := vals[a.Field]; ok && v != nil {
		subject = fmt.Sprint(v)
	}
	if subject != "" && r.Relations != nil {
		who, err := r.Relations.Lookup(ctx, a.Relation, subject)
		if err != nil {
			return Target{}, fmt.Errorf("assign: relation %s(%s): %w", a.Relation, subject, err)
		}
		if len(who) > 0 && who[0] != "" {
			return Target{Assignee: who[0], By: "relation"}, nil
		}
	}
	return Target{Assignee: a.Fallback, By: "fallback"}, nil
}

// PoolResolver：从一批人中按策略选一个。
type PoolResolver struct {
	Pickers map[string]Picker
}

// Kind 实现 Resolver。
func (PoolResolver) Kind() string { return tasktype.AssigneePool }

// Resolve 实现 Resolver。
func (r PoolResolver) Resolve(ctx context.Context, a tasktype.Assignee, vals tasktype.Values) (Target, error) {
	strategy := a.Strategy
	if strategy == "" {
		strategy = tasktype.StrategyRandom
	}
	p, ok := r.Pickers[strategy]
	if !ok {
		return Target{}, fmt.Errorf("assign: unknown strategy %q", strategy)
	}
	if len(a.Users) == 0 {
		return Target{}, fmt.Errorf("assign: pool is empty")
	}
	key := ""
	if v, ok := vals[tasktype.KeyPlaceholder]; ok {
		key = fmt.Sprint(v)
	}
	u, err := p.Pick(ctx, a.Users, key)
	return Target{Assignee: u, By: strategy}, err
}

// ClaimResolver：认领模式，不指定处理人，任务进入待认领。
type ClaimResolver struct{}

// Kind 实现 Resolver。
func (ClaimResolver) Kind() string { return tasktype.AssigneeClaim }

// Resolve 实现 Resolver。
func (ClaimResolver) Resolve(context.Context, tasktype.Assignee, tasktype.Values) (Target, error) {
	return Target{By: tasktype.AssigneeClaim}, nil
}

// WithKey 把任务的去重 key 放进分配器可见的字段（按 key 选人的策略会用到），不写进任务。
func WithKey(vals tasktype.Values, key string) tasktype.Values {
	out := make(tasktype.Values, len(vals)+1)
	for k, v := range vals {
		out[k] = v
	}
	out[tasktype.KeyPlaceholder] = key
	return out
}
