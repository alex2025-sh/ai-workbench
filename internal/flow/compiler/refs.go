package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

// Scope 描述某个位置的表达式能用哪些命名空间。
type Scope struct {
	Roots []string
	// Nodes 校验 nodes.<id> 引用，返回非空字符串表示不合法的原因；为 nil 时不允许使用 nodes。
	Nodes func(id string) string
}

func (s Scope) with(root string) Scope {
	s.Roots = append(append([]string(nil), s.Roots...), root)
	return s
}

// 值为表达式（而不是模板）的字段名。
var exprFields = map[string]bool{"when": true, "anchor": true, "subject": true, "expr": true, "filter": true}

// CheckValue 遍历一段配置 JSON：表达式字段按表达式检查，其余含 {{ 的字符串按模板检查。
// variants[].when 额外允许 viewer。问题按路径排序返回。
func CheckValue(raw json.RawMessage, base string, scope Scope) []Issue {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil // 结构问题由 schema 与解码报告
	}
	var out []Issue
	walkValue(v, base, "", scope, &out)
	return out
}

func walkValue(v any, path, key string, scope Scope, out *[]Issue) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s := scope
			if key == "variants" && k == "when" {
				s = scope.with(expr.RootViewer)
			}
			walkValue(x[k], path+"."+k, k, s, out)
		}
	case []any:
		for i, item := range x {
			itemKey := key
			if key == "exclude" {
				itemKey = "expr"
			}
			if key == "variants" {
				itemKey = "variants"
			}
			walkValue(item, fmt.Sprintf("%s.%d", path, i), itemKey, scope, out)
		}
	case string:
		if exprFields[key] {
			*out = append(*out, CheckExpression(x, path, scope)...)
		} else if strings.Contains(x, "{{") {
			*out = append(*out, CheckTemplate(x, path, scope)...)
		}
	}
}

// CheckExpression 检查一个表达式的语法与引用。
func CheckExpression(src, path string, scope Scope) []Issue {
	if _, err := expr.Compile(src); err != nil {
		return []Issue{{Code: IssueBadExpression, Severity: SeverityError, Path: path, Message: fmt.Sprintf("表达式 %q 有误：%v", src, err)}}
	}
	r, _ := expr.Analyze(src)
	return checkRefs(r, src, path, scope)
}

// CheckTemplate 检查模板中每个 {{ }} 的语法与引用。
func CheckTemplate(src, path string, scope Scope) []Issue {
	r, err := expr.TemplateRefs(src)
	if err != nil {
		return []Issue{{Code: IssueBadExpression, Severity: SeverityError, Path: path, Message: fmt.Sprintf("模板 %q 有误：%v", src, err)}}
	}
	return checkRefs(r, src, path, scope)
}

func checkRefs(r expr.Refs, src, path string, scope Scope) []Issue {
	var out []Issue
	for _, root := range r.Roots {
		if !contains(scope.Roots, root) || (root == expr.RootNodes && scope.Nodes == nil) {
			out = append(out, Issue{Code: IssueBadReference, Severity: SeverityError, Path: path,
				Message: fmt.Sprintf("%q 中的 %q 在这里不可用，可用的命名空间：%s", src, root, strings.Join(scope.Roots, ", "))})
		}
	}
	if scope.Nodes != nil {
		for _, id := range r.Nodes {
			if msg := scope.Nodes(id); msg != "" {
				out = append(out, Issue{Code: IssueBadReference, Severity: SeverityError, Path: path, Message: fmt.Sprintf("%q：%s", src, msg)})
			}
		}
	}
	return out
}
