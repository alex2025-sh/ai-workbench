package assign

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

// StaticRelations 是内存中的业务关系表：关系名 → 主体 → 候选人 email。用于本地开发与测试。
type StaticRelations struct {
	mu   sync.RWMutex
	data map[string]map[string][]string
}

// LoadRelations 从 YAML 文件加载关系表，格式见 deploy/mock/vip_rm.yaml：
//
//	vip_rm:
//	  "10000001": rm.alice@example.com
func LoadRelations(path string) (*StaticRelations, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]map[string]string
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("assign: parse %s: %w", path, err)
	}
	r := &StaticRelations{data: map[string]map[string][]string{}}
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

// Lookup 实现 RelationSource。
func (r *StaticRelations) Lookup(_ context.Context, relation, subjectID string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data[relation][subjectID], nil
}

// Local 是分配服务的本地实现，注册为 action.KindAssign：按策略链依次尝试解析器，取第一个候选人；
// 都没有结果时进入兜底组。过滤器与负载策略在接入 BSP 后实现，这里全部视为通过。
type Local struct {
	Relations RelationSource
}

// Kind 实现 action.Action。
func (Local) Kind() action.Kind { return action.KindAssign }

// Invoke 实现 action.Action。
func (l Local) Invoke(ctx context.Context, req action.Request) (action.Result, error) {
	var b dsl.AssignmentBinding
	if err := json.Unmarshal(req.Config, &b); err != nil {
		return action.Result{}, fmt.Errorf("assign: config: %w", err)
	}
	if b.Inline == nil {
		return action.Result{}, fmt.Errorf("assign: shared policy %q is not supported yet", b.Ref)
	}
	var in action.AssignInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return action.Result{}, fmt.Errorf("assign: input: %w", err)
	}
	payload, _ := expr.Decode(mustJSON(in.Payload))
	task, _ := expr.Decode(mustJSON(in.Task))
	env := expr.Env{expr.RootPayload: payload, expr.RootTask: task}
	out, err := l.resolve(ctx, b.Inline, env)
	if err != nil {
		return action.Result{}, err
	}
	return action.Result{Output: mustJSON(out)}, nil
}

func (l Local) resolve(ctx context.Context, p *dsl.Assignment, env expr.Env) (action.AssignOutput, error) {
	exclude := map[string]bool{}
	for _, src := range p.Exclude {
		for _, who := range evalList(src, env) {
			exclude[who] = true
		}
	}
	for _, r := range p.Resolvers {
		var candidates []string
		switch ResolverType(r.Type) {
		case ResolverRelation:
			subject := evalList(r.Subject, env)
			if len(subject) > 0 && l.Relations != nil {
				found, err := l.Relations.Lookup(ctx, r.Relation, subject[0])
				if err != nil {
					return action.AssignOutput{}, err
				}
				candidates = found
			}
		case ResolverUser:
			candidates = r.Users
		case ResolverGroup:
			return action.AssignOutput{Group: r.Group, ResolvedBy: "group:" + r.Group}, nil
		case ResolverPreviousAssignee:
			if t, ok := env[expr.RootTask].(map[string]any); ok {
				if a, _ := t["assignee"].(string); a != "" {
					candidates = []string{a}
				}
			}
		case ResolverExpr:
			candidates = evalList(r.Expr, env)
		case ResolverAgreement:
			return action.AssignOutput{}, fmt.Errorf("assign: agreement resolver is not supported yet")
		}
		for _, c := range candidates {
			if c = strings.TrimSpace(c); c != "" && !exclude[c] {
				by := r.Type
				if r.Relation != "" {
					by += ":" + r.Relation
				}
				return action.AssignOutput{Assignee: c, ResolvedBy: by}, nil
			}
		}
	}
	if p.Fallback != nil {
		return action.AssignOutput{Group: p.Fallback.Group, ResolvedBy: "fallback"}, nil
	}
	return action.AssignOutput{}, fmt.Errorf("assign: no candidate and no fallback")
}

func evalList(src string, env expr.Env) []string {
	if src == "" {
		return nil
	}
	p, err := expr.Compile(src)
	if err != nil {
		return nil
	}
	v, err := p.Run(env)
	if err != nil || v == nil {
		return nil
	}
	switch x := v.(type) {
	case []any:
		var out []string
		for _, i := range x {
			out = append(out, fmt.Sprint(i))
		}
		return out
	default:
		return []string{fmt.Sprint(x)}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
