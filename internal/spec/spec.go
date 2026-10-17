// Package spec 是任务类型定义（SaveTaskTypeDraft.body_json）的 Go 模型与完整校验。
//
// 校验分三层，问题统一用 compiler.Issue 返回，Path 用 ID 定位（如 workflow.nodes.ask_rm.config.timeout）：
//  1. 结构：按 api/schema/task_type.schema.json 校验；
//  2. 语义：payload schema、来源、去重键、卡片模板及其引用；
//  3. workflow：交给 compiler 编译，产出执行计划。
package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	apischema "github.com/alex2025-sh/ai-workbench/api/schema"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/schema"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/yamljson"
	"github.com/alex2025-sh/ai-workbench/internal/view"
)

// TaskType 是一个任务类型版本的完整定义。
type TaskType struct {
	Key             string                 `json:"key"`
	Namespace       string                 `json:"namespace,omitempty"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description,omitempty"`
	DefaultPriority string                 `json:"default_priority,omitempty"`
	Schema          json.RawMessage        `json:"schema"`
	Sources         []Source               `json:"sources,omitempty"`
	DedupKey        string                 `json:"dedup_key,omitempty"`
	Views           map[string]ViewEntry   `json:"views,omitempty"`
	View            *dsl.ViewBinding       `json:"view,omitempty"`
	Assignment      *dsl.AssignmentBinding `json:"assignment,omitempty"`
	Workflow        dsl.Graph              `json:"workflow"`
	UI              json.RawMessage        `json:"ui,omitempty"`
}

// Priority 返回默认优先级，未配置时为 P2。
func (t *TaskType) Priority() string {
	if t.DefaultPriority == "" {
		return "P2"
	}
	return t.DefaultPriority
}

// 来源类型。
const (
	SourceCron  = "cron"
	SourceKafka = "kafka"
	SourceAPI   = "api"
)

// Source 是任务来源。cron 用 Schedule / Timezone / Skill / Input；kafka 与 api 用 EventType。
type Source struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Schedule  string            `json:"schedule,omitempty"`
	Timezone  string            `json:"timezone,omitempty"`
	Skill     string            `json:"skill,omitempty"`
	Input     map[string]any    `json:"input,omitempty"`
	EventType string            `json:"event_type,omitempty"`
	Filter    string            `json:"filter,omitempty"`  // 表达式，基于 record
	Mapping   map[string]string `json:"mapping,omitempty"` // payload 字段 → 表达式
}

// ViewEntry 是 views 中的一项：内嵌模板（自有组件）或共享组件引用 key@version。
type ViewEntry struct {
	Ref    string
	Inline *view.Template
}

func (v ViewEntry) MarshalJSON() ([]byte, error) {
	if v.Inline != nil {
		return json.Marshal(v.Inline)
	}
	return json.Marshal(v.Ref)
}

func (v *ViewEntry) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	*v = ViewEntry{}
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &v.Ref)
	}
	v.Inline = &view.Template{}
	return strict(b, v.Inline)
}

// Parse 严格解码任务类型 JSON（未知字段报错，数字保留为 json.Number）。不做校验。
func Parse(body []byte) (*TaskType, error) {
	var t TaskType
	if err := strict(body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// FromYAML 把 YAML 写法转成提交给 SaveTaskTypeDraft 的 JSON。
func FromYAML(raw []byte) ([]byte, error) { return yamljson.Convert(raw) }

func strict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	return dec.Decode(v)
}

// Result 是一次完整校验的结果。存在 error 级别的问题时 Plan 为 nil；结构校验失败时 Spec 也为 nil。
type Result struct {
	Spec   *TaskType
	Plan   *compiler.Plan
	Issues []compiler.Issue
}

// OK 报告是否没有 error 级别的问题。
func (r Result) OK() bool { return !compiler.HasErrors(r.Issues) }

// Check 对任务类型 JSON 做完整校验并编译 workflow。
func Check(body []byte) Result {
	vs, err := apischema.ValidateTaskType(body)
	if err != nil {
		return Result{Issues: []compiler.Issue{{Code: compiler.IssueSchema, Severity: compiler.SeverityError, Message: err.Error()}}}
	}
	if len(vs) > 0 {
		ids := collectIDs(body)
		var issues []compiler.Issue
		for _, v := range vs {
			issues = append(issues, compiler.Issue{Code: compiler.IssueSchema, Severity: compiler.SeverityError, Path: ids.path(v.Location), Message: v.Message})
		}
		return Result{Issues: issues}
	}
	t, err := Parse(body)
	if err != nil {
		return Result{Issues: []compiler.Issue{{Code: compiler.IssueSchema, Severity: compiler.SeverityError, Message: err.Error()}}}
	}
	c := &checker{t: t}
	c.run()
	plan, wfIssues := compiler.Compile(t.Workflow, compiler.Defaults{View: t.View, Assignment: t.Assignment})
	c.issues = append(c.issues, wfIssues...)
	if plan != nil {
		c.checkViewRefs(plan)
	}
	res := Result{Spec: t, Issues: c.issues}
	if res.OK() {
		res.Plan = plan
	}
	return res
}

type checker struct {
	t      *TaskType
	props  map[string]json.RawMessage
	issues []compiler.Issue
}

func (c *checker) errorf(code compiler.IssueCode, path, format string, args ...any) {
	c.issues = append(c.issues, compiler.Issue{Code: code, Severity: compiler.SeverityError, Path: path, Message: fmt.Sprintf(format, args...)})
}

func (c *checker) warnf(code compiler.IssueCode, path, format string, args ...any) {
	c.issues = append(c.issues, compiler.Issue{Code: code, Severity: compiler.SeverityWarning, Path: path, Message: fmt.Sprintf(format, args...)})
}

func (c *checker) add(issues []compiler.Issue) { c.issues = append(c.issues, issues...) }

var (
	scopeRecord  = compiler.Scope{Roots: []string{expr.RootRecord}}
	scopePayload = compiler.Scope{Roots: []string{expr.RootPayload}}
	scopeTask    = compiler.Scope{Roots: []string{expr.RootPayload, expr.RootTask}}
)

func (c *checker) run() {
	c.checkPayloadSchema()
	c.checkSources()
	if c.t.DedupKey != "" {
		c.add(compiler.CheckTemplate(c.t.DedupKey, "dedup_key", scopePayload))
	} else if len(c.t.Sources) > 0 {
		c.warnf(compiler.IssueUnused, "dedup_key", "建议配置 dedup_key，否则同一条数据被重复扫描时会重复创建任务")
	}
	c.checkViews()
	if c.t.View != nil {
		raw, _ := json.Marshal(c.t.View)
		c.add(compiler.CheckValue(raw, "view", scopeTask))
	}
	if c.t.Assignment != nil {
		raw, _ := json.Marshal(c.t.Assignment)
		c.add(compiler.CheckValue(raw, "assignment", scopeTask))
	}
}

// ---- payload schema ----

var piiValues = map[string]bool{string(schema.PIIMaskMiddle): true, string(schema.PIIHash): true, string(schema.PIIDrop): true}
var formatValues = map[string]bool{string(schema.FormatMoney): true, string(schema.FormatPercent): true, string(schema.FormatDuration): true, string(schema.FormatDatetime): true}

func (c *checker) checkPayloadSchema() {
	if err := apischema.ValidateSchema(c.t.Schema); err != nil {
		c.errorf(compiler.IssueSchema, "schema", "payload schema 不是合法的 JSON Schema：%v", err)
		return
	}
	var root struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	_ = json.Unmarshal(c.t.Schema, &root)
	c.props = root.Properties
	for _, r := range root.Required {
		if _, ok := c.props[r]; !ok {
			c.errorf(compiler.IssueBadReference, "schema.required", "必填字段 %q 没有在 properties 中定义", r)
		}
	}
	c.checkProperties(root.Properties, "schema.properties")
}

func (c *checker) checkProperties(props map[string]json.RawMessage, base string) {
	for _, name := range sortedKeys(props) {
		p := base + "." + name
		var f struct {
			Type          any                        `json:"type"`
			PII           *string                    `json:"x-pii"`
			Format        *string                    `json:"x-format"`
			CurrencyField *string                    `json:"x-currency-field"`
			Properties    map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(props[name], &f); err != nil {
			continue
		}
		if f.PII != nil && !piiValues[*f.PII] {
			c.errorf(compiler.IssueSchema, p+".x-pii", "x-pii 只能是 mask_middle、hash、drop，得到 %q", *f.PII)
		}
		if f.Format != nil {
			if !formatValues[*f.Format] {
				c.errorf(compiler.IssueSchema, p+".x-format", "x-format 只能是 money、percent、duration、datetime，得到 %q", *f.Format)
			} else if *f.Format == string(schema.FormatMoney) && f.Type != "string" {
				c.errorf(compiler.IssueSchema, p+".type", "金额字段 %q 的类型必须是 string，避免精度丢失", name)
			}
		}
		if f.CurrencyField != nil {
			if _, ok := props[*f.CurrencyField]; !ok {
				c.errorf(compiler.IssueBadReference, p+".x-currency-field", "币种字段 %q 不存在", *f.CurrencyField)
			}
		}
		if len(f.Properties) > 0 {
			c.checkProperties(f.Properties, p+".properties")
		}
	}
}

// ---- 来源 ----

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func (c *checker) checkSources() {
	seen := map[string]bool{}
	for _, s := range c.t.Sources {
		p := "sources." + s.ID
		if seen[s.ID] {
			c.errorf(compiler.IssueDuplicateID, p, "来源 ID %q 重复", s.ID)
			continue
		}
		seen[s.ID] = true
		if s.Kind == SourceCron {
			if _, err := cronParser.Parse(s.Schedule); err != nil {
				c.errorf(compiler.IssueBadConfig, p+".schedule", "cron 表达式 %q 有误：%v", s.Schedule, err)
			}
			if s.Timezone != "" {
				if _, err := time.LoadLocation(s.Timezone); err != nil {
					c.errorf(compiler.IssueBadConfig, p+".timezone", "未知时区 %q", s.Timezone)
				}
			}
			raw, _ := json.Marshal(s.Input)
			c.add(compiler.CheckValue(raw, p+".input", compiler.Scope{}))
		}
		if s.Filter != "" {
			c.add(compiler.CheckExpression(s.Filter, p+".filter", scopeRecord))
		}
		for _, field := range sortedKeys(s.Mapping) {
			fp := p + ".mapping." + field
			if c.props != nil {
				if _, ok := c.props[field]; !ok {
					c.errorf(compiler.IssueBadReference, fp, "字段 %q 不在 payload schema 中", field)
				}
			}
			c.add(compiler.CheckExpression(s.Mapping[field], fp, scopeRecord))
		}
		if c.props != nil {
			var root struct {
				Required []string `json:"required"`
			}
			_ = json.Unmarshal(c.t.Schema, &root)
			for _, r := range root.Required {
				if _, ok := s.Mapping[r]; !ok {
					c.warnf(compiler.IssueBadReference, p+".mapping", "必填字段 %q 没有映射，这个来源产生的数据会校验失败", r)
				}
			}
		}
	}
}

// ---- 卡片模板 ----

func (c *checker) checkViews() {
	nodes := map[string]bool{}
	for _, n := range c.t.Workflow.Nodes {
		nodes[n.ID] = true
	}
	scope := compiler.Scope{
		Roots: []string{expr.RootPayload, expr.RootTask, expr.RootNodes, expr.RootLoop},
		Nodes: func(id string) string {
			if !nodes[id] {
				return fmt.Sprintf("节点 %q 不存在", id)
			}
			return ""
		},
	}
	for _, name := range sortedKeys(c.t.Views) {
		v := c.t.Views[name].Inline
		if v == nil {
			continue
		}
		raw, _ := json.Marshal(v)
		c.add(compiler.CheckValue(raw, "views."+name, scope))
		if v.AISummary != nil && c.props != nil {
			for i, in := range v.AISummary.Inputs {
				if _, ok := c.props[in]; !ok {
					c.errorf(compiler.IssueBadReference, fmt.Sprintf("views.%s.ai_summary.inputs.%d", name, i), "字段 %q 不在 payload schema 中", in)
				}
			}
		}
	}
}

// checkViewRefs 检查所有引用到的本地模板名都已在 views 中定义，并提示未使用的模板。
func (c *checker) checkViewRefs(plan *compiler.Plan) {
	used := map[string]bool{}
	ref := func(name, path string) {
		if strings.Contains(name, "@") {
			return // 共享组件，存在性在发布时按依赖闭包检查
		}
		used[name] = true
		if _, ok := c.t.Views[name]; !ok {
			c.errorf(compiler.IssueBadReference, path, "卡片模板 %q 没有在 views 中定义", name)
		}
	}
	if c.t.View != nil {
		for _, n := range c.t.View.Names() {
			ref(n, "view")
		}
	}
	for _, n := range c.t.Workflow.Nodes {
		p := "workflow.nodes." + n.ID + ".config.view"
		switch n.Type {
		case dsl.NodeHumanChoice, dsl.NodeLarkCard:
			if b, ok := plan.Bindings[n.ID]; ok && b.ViewFrom == compiler.FromSelf && b.View != nil {
				for _, name := range b.View.Names() {
					ref(name, p)
				}
			}
		case dsl.NodeSendEmail:
			cfg, err := dsl.DecodeConfig(n.Type, n.Config)
			if err == nil && cfg.(*dsl.SendEmailConfig).View != "" {
				ref(cfg.(*dsl.SendEmailConfig).View, p)
			}
		}
	}
	for _, name := range sortedKeys(c.t.Views) {
		if !used[name] {
			c.warnf(compiler.IssueUnused, "views."+name, "卡片模板 %q 没有被使用", name)
		}
	}
}

// ---- 路径 ----

// idIndex 把 schema 校验给出的数组下标换成 ID，使路径与编译器的一致。
type idIndex struct {
	nodes, edges, sources []string
}

func collectIDs(body []byte) idIndex {
	var doc struct {
		Sources []struct {
			ID any `json:"id"`
		} `json:"sources"`
		Workflow struct {
			Nodes []struct {
				ID any `json:"id"`
			} `json:"nodes"`
			Edges []struct {
				ID any `json:"id"`
			} `json:"edges"`
		} `json:"workflow"`
	}
	_ = json.Unmarshal(body, &doc)
	var ix idIndex
	str := func(v any) string { s, _ := v.(string); return s }
	for _, s := range doc.Sources {
		ix.sources = append(ix.sources, str(s.ID))
	}
	for _, n := range doc.Workflow.Nodes {
		ix.nodes = append(ix.nodes, str(n.ID))
	}
	for _, e := range doc.Workflow.Edges {
		ix.edges = append(ix.edges, str(e.ID))
	}
	return ix
}

func (ix idIndex) path(loc []string) string {
	out := append([]string(nil), loc...)
	swap := func(i int, ids []string) {
		if i < len(out) {
			var n int
			if _, err := fmt.Sscanf(out[i], "%d", &n); err == nil && n < len(ids) && ids[n] != "" {
				out[i] = ids[n]
			}
		}
	}
	switch {
	case len(out) >= 2 && out[0] == "sources":
		swap(1, ix.sources)
	case len(out) >= 3 && out[0] == "workflow" && out[1] == "nodes":
		swap(2, ix.nodes)
	case len(out) >= 3 && out[0] == "workflow" && out[1] == "edges":
		swap(2, ix.edges)
	}
	return strings.Join(out, ".")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
