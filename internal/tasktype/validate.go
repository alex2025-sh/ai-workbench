package tasktype

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/alex2025-sh/ai-workbench/api/schema"
)

// Issue 是一个校验问题。Path 是 JSON 路径（如 nodes.1.link），前端据此定位到表单项。
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (i Issue) String() string { return i.Path + ": " + i.Message }

var compiled = func() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.TaskType))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("task_type.schema.json", doc); err != nil {
		panic(err)
	}
	return c.MustCompile("task_type.schema.json")
}()

var printer = message.NewPrinter(language.English)

func errorsAs(err error, target **jsonschema.ValidationError) bool { return errors.As(err, target) }

// CronParser 解析 5 段 cron（分 时 日 月 周）。
var CronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Validate 校验任务类型 JSON：先按 JSON Schema 校验结构，结构没问题再做语义校验。
// 没有问题时返回解析结果；有问题时 TaskType 可能为 nil。
func Validate(raw []byte) (*TaskType, []Issue) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, []Issue{{Path: "", Message: "不是合法的 JSON：" + err.Error()}}
	}
	if err := compiled.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errorsAs(err, &ve) {
			return nil, schemaIssues(ve)
		}
		return nil, []Issue{{Message: err.Error()}}
	}
	t, err := Parse(raw)
	if err != nil {
		return nil, []Issue{{Message: err.Error()}}
	}
	return t, Check(t)
}

// Check 做语义校验：字段、模板引用、节点链与数据源。
func Check(t *TaskType) []Issue {
	var out []Issue
	add := func(path, format string, args ...any) {
		out = append(out, Issue{Path: path, Message: fmt.Sprintf(format, args...)})
	}

	fields := map[string]Field{}
	for i, f := range t.Fields {
		p := fmt.Sprintf("fields.%d", i)
		if f.Name == KeyPlaceholder {
			add(p+".name", "key 是数据源给出的去重键，不能作为业务字段名")
		}
		if _, dup := fields[f.Name]; dup {
			add(p+".name", "字段 %s 重复", f.Name)
		}
		fields[f.Name] = f
		if f.Type == FieldEnum && len(f.Options) == 0 {
			add(p+".options", "枚举字段需要列出取值")
		}
		if f.Type != FieldEnum && len(f.Options) > 0 {
			add(p+".options", "只有枚举字段可以设置取值")
		}
	}
	known := func(name string) bool { _, ok := fields[name]; return ok || name == KeyPlaceholder }
	checkTemplate := func(path, tpl string) {
		for _, name := range Placeholders(tpl) {
			if !known(name) {
				add(path, "引用了不存在的字段 {%s}", name)
			}
		}
	}

	checkTemplate("card.title", t.Card.Title)
	for i, name := range t.Card.Fields {
		if _, ok := fields[name]; !ok {
			add(fmt.Sprintf("card.fields.%d", i), "字段 %s 不存在", name)
		}
	}

	ids := map[string]bool{}
	for i, n := range t.Nodes {
		p := fmt.Sprintf("nodes.%d", i)
		if ids[n.ID] {
			add(p+".id", "节点 id %s 重复", n.ID)
		}
		ids[n.ID] = true
		last := i == len(t.Nodes)-1
		if n.Has(ActionEscalate) && last {
			add(p+".actions", "最后一个节点没有下一个节点，不能启用 escalate")
		}
		if !n.Has(ActionClose) && !n.Has(ActionNoResponse) && !n.Has(ActionEscalate) {
			add(p+".actions", "节点至少要有 close、no_response、escalate 之一，否则任务无法离开该节点")
		}
		switch {
		case n.Has(ActionLink) && n.Link == nil:
			add(p+".link", "启用了 link，需要填写按钮文字和链接")
		case !n.Has(ActionLink) && n.Link != nil:
			add(p+".link", "没有启用 link，请删除链接配置")
		case n.Link != nil:
			checkTemplate(p+".link.url", n.Link.URL)
		}
		if len(n.Snooze) > 0 && !n.Has(ActionSnooze) {
			add(p+".snooze", "没有启用 snooze，请删除推迟时长")
		}
		for j, d := range n.Snooze {
			if _, err := ParseDuration(d); err != nil {
				add(fmt.Sprintf("%s.snooze.%d", p, j), "%v", err)
			}
		}
		if n.Assignee.Kind == AssigneeRelation {
			if _, ok := fields[n.Assignee.Field]; !ok {
				add(p+".assignee.field", "字段 %s 不存在", n.Assignee.Field)
			}
		}
	}

	if t.Source.Kind == SourceSQL {
		if _, err := CronParser.Parse(t.Source.Schedule); err != nil {
			add("source.schedule", "cron 表达式不合法：%v", err)
		}
		if tz := t.Source.Timezone; tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				add("source.timezone", "未知时区 %s", tz)
			}
		}
		if err := CheckQuery(t.Source.Query); err != nil {
			add("source.query", "%v", err)
		}
	}
	return out
}

var (
	lineComment  = regexp.MustCompile(`--[^\n]*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	forbidden    = regexp.MustCompile(`(?i)\b(insert|update|delete|replace|drop|alter|create|truncate|grant|revoke|call|set|lock|unlock|load|handler|into\s+outfile|for\s+update|lock\s+in\s+share\s+mode)\b`)
)

// CheckQuery 粗检 SQL 来源的查询：只能是单条 SELECT（可用 WITH）。
// 这只是尽早给出提示，真正的保护是执行时的只读事务、超时与只读从库账号。
func CheckQuery(q string) error {
	s := strings.TrimSpace(blockComment.ReplaceAllString(lineComment.ReplaceAllString(q, ""), " "))
	s = strings.TrimSuffix(s, ";")
	if s == "" {
		return fmt.Errorf("查询为空")
	}
	if strings.Contains(s, ";") {
		return fmt.Errorf("只能是单条语句")
	}
	head := strings.ToLower(strings.Fields(s)[0])
	if head != "select" && head != "with" {
		return fmt.Errorf("只能是 SELECT 查询")
	}
	if m := forbidden.FindString(stripStrings(s)); m != "" {
		return fmt.Errorf("查询中不能包含 %s", strings.ToUpper(m))
	}
	return nil
}

// stripStrings 去掉字符串字面量，避免把字符串里的单词当成关键字。
func stripStrings(s string) string {
	var b strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func schemaIssues(ve *jsonschema.ValidationError) []Issue {
	var out []Issue
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			out = append(out, schemaIssue(e))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return dedup(out)
}

// fieldNames 是常见路径末段的中文名，用于把 JSON Schema 的报错翻译成配置人员看得懂的话。
var fieldNames = map[string]string{
	"key": "标识", "name": "名称", "fallback": "兜底人", "users": "候选人", "strategy": "选择策略", "relation": "业务关系", "field": "字段",
	"datasource": "数据源连接", "schedule": "调度", "query": "查询", "title": "标题", "label": "显示名", "url": "链接地址",
	"id": "节点 id", "actions": "动作", "nodes": "节点", "kind": "类型", "type": "类型", "pii": "脱敏",
}

// schemaIssue 把一条 JSON Schema 报错翻译成中文；不认识的保持原文。
func schemaIssue(e *jsonschema.ValidationError) Issue {
	loc := e.InstanceLocation
	path := strings.Join(loc, ".")
	name := ""
	if len(loc) > 0 {
		name = fieldNames[loc[len(loc)-1]]
	}
	msg := e.ErrorKind.LocalizedString(printer)
	switch k := e.ErrorKind.(type) {
	case *kind.Required:
		var names []string
		for _, m := range k.Missing {
			if n := fieldNames[m]; n != "" {
				names = append(names, n)
			} else {
				names = append(names, m)
			}
		}
		msg = "缺少" + strings.Join(names, "、")
		if len(k.Missing) == 1 {
			path = strings.TrimPrefix(path+"."+k.Missing[0], ".")
		}
	case *kind.Pattern:
		if k.Got == "" {
			msg = "不能为空"
		} else {
			msg = fmt.Sprintf("「%s」格式不正确：只能用小写字母、数字、下划线，字母开头", k.Got)
		}
	case *kind.MinLength:
		msg = "不能为空"
	case *kind.MinItems:
		msg = fmt.Sprintf("至少需要 %d 项", k.Want)
	case *kind.Enum:
		msg = fmt.Sprintf("取值 %v 不在可选范围内", k.Got)
	case *kind.AdditionalProperties:
		msg = "不支持的配置项：" + strings.Join(k.Properties, "、")
	case *kind.UniqueItems:
		msg = "有重复项"
	case *kind.Type:
		msg = fmt.Sprintf("类型应为 %s", strings.Join(k.Want, " / "))
	}
	if name != "" && !strings.HasPrefix(msg, "缺少") && !strings.HasPrefix(msg, "不支持") {
		msg = name + msg
	}
	return Issue{Path: path, Message: msg}
}

func dedup(in []Issue) []Issue {
	var out []Issue
	seen := map[string]bool{}
	for _, i := range in {
		if k := i.String(); !seen[k] {
			seen[k] = true
			out = append(out, i)
		}
	}
	return out
}

// ParseBody 解析 JSON 并返回格式化前的原文，用于计算内容哈希等场景。
func ParseBody(raw []byte) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}
