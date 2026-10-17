// Package apischema 提供任务类型 JSON 的契约：task_type.schema.json。
//
// 这份 schema 是前后端之间关于 SaveTaskTypeDraft.body_json 的约定，前端可以直接用它生成表单、做本地校验；
// 后端在保存草稿、校验、发布时用同一份 schema 做结构校验，再交给编译器做语义校验。
package apischema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var printer = message.NewPrinter(language.English)

//go:embed task_type.schema.json
var taskTypeSchema []byte

// SchemaID 是任务类型 schema 的 $id。
const SchemaID = "https://schemas.ai-workbench/task_type.schema.json"

// TaskTypeSchema 返回 schema 原文。
func TaskTypeSchema() []byte { return bytes.Clone(taskTypeSchema) }

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
)

func taskType() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiled, compileErr = compileDoc(SchemaID, taskTypeSchema)
	})
	return compiled, compileErr
}

func compileDoc(id string, raw []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("apischema: parse %s: %w", id, err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource(id, doc); err != nil {
		return nil, fmt.Errorf("apischema: add %s: %w", id, err)
	}
	s, err := c.Compile(id)
	if err != nil {
		return nil, fmt.Errorf("apischema: compile %s: %w", id, err)
	}
	return s, nil
}

// Violation 是一条结构校验错误。
type Violation struct {
	// Location 是出错值在文档中的位置，如 ["workflow", "nodes", "3", "config", "timeout"]。
	Location []string
	Message  string
}

// ValidateTaskType 按 task_type.schema.json 校验一份任务类型 JSON。
// 返回的 error 只表示 JSON 无法解析或 schema 本身有问题；结构错误放在 Violation 里。
func ValidateTaskType(body []byte) ([]Violation, error) {
	s, err := taskType()
	if err != nil {
		return nil, err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("apischema: invalid json: %w", err)
	}
	return violations(s.Validate(inst)), nil
}

// ValidateSchema 校验一份 payload JSON Schema 本身是否是合法的 draft 2020-12 schema。
func ValidateSchema(raw []byte) error {
	_, err := compileDoc("https://schemas.ai-workbench/payload.schema.json", raw)
	return err
}

func violations(err error) []Violation {
	if err == nil {
		return nil
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []Violation{{Message: err.Error()}}
	}
	seen := map[string]bool{}
	var out []Violation
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			v := Violation{Location: append([]string(nil), e.InstanceLocation...), Message: e.ErrorKind.LocalizedString(printer)}
			k := strings.Join(v.Location, "/") + "\x00" + v.Message
			if !seen[k] {
				seen[k] = true
				out = append(out, v)
			}
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Join(out[i].Location, "/") < strings.Join(out[j].Location, "/")
	})
	return out
}

// 节点配置在 schema 中的定义名。
var nodeConfigDefs = map[string]string{
	"trigger":      "config_empty",
	"condition":    "config_condition",
	"merge":        "config_merge",
	"timer":        "config_timer",
	"human_choice": "config_human_choice",
	"skill_call":   "config_skill_call",
	"llm":          "config_llm",
	"lark_card":    "config_lark_card",
	"send_email":   "config_send_email",
	"emit_event":   "config_emit_event",
	"end":          "config_end",
}

// NodeConfigSchema 返回某个节点类型配置的独立 JSON Schema（带上全部 $defs），
// 用于 CatalogService.ListNodeTypes 的 config_schema_json。
func NodeConfigSchema(nodeType string) ([]byte, error) {
	def, ok := nodeConfigDefs[nodeType]
	if !ok {
		return nil, fmt.Errorf("apischema: unknown node type %q", nodeType)
	}
	var root struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(taskTypeSchema, &root); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://schemas.ai-workbench/nodes/" + nodeType + ".schema.json",
		"$ref":    "#/$defs/" + def,
		"$defs":   root.Defs,
	})
}
