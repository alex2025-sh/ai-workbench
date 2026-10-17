// Package expr 是 DSL 中条件、映射、模板表达式的编译、静态分析与求值，基于 expr-lang/expr。
//
// 表达式必须是纯函数：禁用 now()，当前时间由引擎的 Clock 注入，保证推进过程可复现。
//
// 两种写法：
//   - 表达式：直接写，如 `payload.vip_level > 0`，用于 when、anchor、filter、subject、expr、exclude、mapping。
//   - 模板：字符串中用 {{ 表达式 }} 插值，如 `VIP 工单 {{ payload.appeal_id }}`，其余字符串字段都是模板。
package expr

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// Env 是求值环境，顶层键即命名空间：payload、task、nodes、loop、viewer、record。
type Env map[string]any

// 命名空间。
const (
	RootPayload = "payload"
	RootTask    = "task"
	RootNodes   = "nodes"
	RootLoop    = "loop"
	RootViewer  = "viewer"
	RootRecord  = "record"
)

// ErrImpure 表示表达式读取了当前时间等非确定性的值。
var ErrImpure = errors.New("expr: now() is not allowed; time comes from the engine clock")

// Program 是预编译后的表达式。
type Program struct {
	src string
	p   *vm.Program
}

// Source 返回表达式原文。
func (p *Program) Source() string { return p.src }

// Run 在给定环境中求值。
func (p *Program) Run(env Env) (any, error) {
	return expr.Run(p.p, map[string]any(env))
}

// Compile 编译一个表达式；语法错误或使用 now() 时返回错误。
func Compile(src string) (*Program, error) {
	if _, err := Analyze(src); err != nil {
		return nil, err
	}
	p, err := expr.Compile(src, expr.DisableBuiltin("now"))
	if err != nil {
		return nil, err
	}
	return &Program{src: src, p: p}, nil
}

// Refs 是表达式引用的外部数据。
type Refs struct {
	Roots []string // 用到的顶层命名空间，去重排序
	Nodes []string // nodes.<id> 中的节点 ID，去重排序
}

func (r *Refs) merge(o Refs) {
	r.Roots = uniq(append(r.Roots, o.Roots...))
	r.Nodes = uniq(append(r.Nodes, o.Nodes...))
}

// Analyze 解析表达式并返回它引用的命名空间与节点。
func Analyze(src string) (Refs, error) {
	if strings.TrimSpace(src) == "" {
		return Refs{}, errors.New("expr: empty expression")
	}
	tree, err := parser.Parse(src)
	if err != nil {
		return Refs{}, err
	}
	v := &refVisitor{}
	ast.Walk(&tree.Node, v)
	if v.impure {
		return Refs{}, ErrImpure
	}
	return Refs{Roots: uniq(v.roots), Nodes: uniq(v.nodes)}, nil
}

// refVisitor 收集标识符与 nodes.<id> 引用。ast.Walk 是后序遍历；调用未知函数时函数名也会被当作命名空间，
// 由调用方按未知命名空间报错，这正是期望的结果（DSL 只允许内置函数）。
type refVisitor struct {
	roots  []string
	nodes  []string
	impure bool
}

func (v *refVisitor) Visit(n *ast.Node) {
	switch node := (*n).(type) {
	case *ast.BuiltinNode:
		if node.Name == "now" {
			v.impure = true
		}
	case *ast.IdentifierNode:
		v.roots = append(v.roots, node.Value)
	case *ast.MemberNode:
		if id, ok := node.Node.(*ast.IdentifierNode); ok && id.Value == RootNodes {
			if s, ok := node.Property.(*ast.StringNode); ok {
				v.nodes = append(v.nodes, s.Value)
			}
		}
	}
}

// Template 是解析后的字符串模板。
type Template struct {
	parts []part
}

type part struct {
	lit  string
	prog *Program
}

// ParseTemplate 解析模板，编译其中每个 {{ }}。不含 {{ 的字符串是纯文本。
func ParseTemplate(s string) (*Template, error) {
	t := &Template{}
	for len(s) > 0 {
		i := strings.Index(s, "{{")
		if i < 0 {
			t.parts = append(t.parts, part{lit: s})
			break
		}
		if i > 0 {
			t.parts = append(t.parts, part{lit: s[:i]})
		}
		j := strings.Index(s[i+2:], "}}")
		if j < 0 {
			return nil, fmt.Errorf("expr: unclosed {{ in template %q", s)
		}
		src := strings.TrimSpace(s[i+2 : i+2+j])
		p, err := Compile(src)
		if err != nil {
			return nil, fmt.Errorf("expr: in {{ %s }}: %w", src, err)
		}
		t.parts = append(t.parts, part{prog: p})
		s = s[i+2+j+2:]
	}
	return t, nil
}

// TemplateRefs 返回模板中所有表达式的引用。
func TemplateRefs(s string) (Refs, error) {
	t, err := ParseTemplate(s)
	if err != nil {
		return Refs{}, err
	}
	var r Refs
	for _, p := range t.parts {
		if p.prog != nil {
			pr, _ := Analyze(p.prog.src)
			r.merge(pr)
		}
	}
	return r, nil
}

// Render 求值并拼接模板。nil 渲染为空字符串。
func (t *Template) Render(env Env) (string, error) {
	var b strings.Builder
	for _, p := range t.parts {
		if p.prog == nil {
			b.WriteString(p.lit)
			continue
		}
		v, err := p.prog.Run(env)
		if err != nil {
			return "", err
		}
		if v != nil {
			fmt.Fprint(&b, v)
		}
	}
	return b.String(), nil
}

func uniq(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	sort.Strings(s)
	out := s[:1]
	for _, x := range s[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}
