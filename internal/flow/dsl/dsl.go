// Package dsl 定义 workflow 的图模型：节点 + 端口 + 连线，以及各节点类型的配置结构。
//
// 画布、仓库中的 YAML 与 AI 生成的是同一份 DSL，结构契约见 api/schema/task_type.schema.json；
// DSL 只描述结构与配置，不包含脚本。设计见 docs/workflow-engine.md「DSL 与编译器」「受控的环」。
package dsl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NodeType 是节点类型。
type NodeType string

const (
	// 原生节点：使用引擎原语，数量少、写好后基本不动。
	NodeTrigger     NodeType = "trigger"
	NodeCondition   NodeType = "condition"
	NodeMerge       NodeType = "merge"
	NodeTimer       NodeType = "timer"
	NodeHumanChoice NodeType = "human_choice"
	NodeEnd         NodeType = "end"

	// 动作节点：统一经 InvokeAction 执行外部 IO。
	NodeSkillCall NodeType = "skill_call"
	NodeLLM       NodeType = "llm"
	NodeLarkCard  NodeType = "lark_card"
	NodeSendEmail NodeType = "send_email"
	NodeEmitEvent NodeType = "emit_event"
)

// 通用端口名。各节点类型还会声明自己的端口（human_choice 的每个选项、condition 的每个分支）。
const (
	PortNext    = "next"
	PortError   = "error"
	PortTimeout = "timeout"
	PortDefault = "default"
)

// EdgeKind 区分普通边与受控的回边。
type EdgeKind string

const (
	EdgeNormal EdgeKind = "normal" // 普通边：必须构成 DAG；空值等同 normal
	EdgeRework EdgeKind = "rework" // 退回边：显式声明，源节点须为 human_choice
)

// Graph 是一个 workflow 的完整图。workflow 是任务类型的自有组件，没有独立的 ID 与版本。
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Node 是图中的一个节点。Config 的结构由节点类型决定，用 DecodeConfig 解码。
type Node struct {
	ID          string          `json:"id"`
	Type        NodeType        `json:"type"`
	Name        string          `json:"name,omitempty"`
	Config      json.RawMessage `json:"config,omitempty"`
	Retry       *RetryPolicy    `json:"retry,omitempty"`
	SideEffects []SideEffect    `json:"side_effects,omitempty"`
}

// Edge 从某节点的某端口连到另一节点。
type Edge struct {
	ID   string   `json:"id"`
	From string   `json:"from"`
	Port string   `json:"port"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind,omitempty"`

	// 以下仅对 rework 边有效，且为必填。
	MaxIterations int    `json:"max_iterations,omitempty"`
	OnExhausted   string `json:"on_exhausted,omitempty"` // 超过次数后的去向节点
}

// IsRework 报告是否为退回边。
func (e Edge) IsRework() bool { return e.Kind == EdgeRework }

// SideEffectMode 控制旁路动作是否阻塞主路径。
type SideEffectMode string

const (
	SideEffectAsync SideEffectMode = "async"
	SideEffectAwait SideEffectMode = "await"
)

// SideEffectOnFailure 是旁路动作失败时的策略。
type SideEffectOnFailure string

const (
	SideEffectFail   SideEffectOnFailure = "fail"
	SideEffectAlert  SideEffectOnFailure = "alert"
	SideEffectIgnore SideEffectOnFailure = "ignore"
)

// SideEffect 是挂在节点上的旁路动作，Config 的结构与同名动作节点相同。
type SideEffect struct {
	Action    NodeType            `json:"action"`
	Config    json.RawMessage     `json:"config"`
	Mode      SideEffectMode      `json:"mode,omitempty"`
	OnFailure SideEffectOnFailure `json:"on_failure,omitempty"`
}

// RetryPolicy 的字段参考 Temporal。各节点类型有默认值，DSL 只能在平台允许的范围内覆盖。
type RetryPolicy struct {
	InitialInterval    Duration `json:"initial_interval,omitempty"`
	BackoffCoefficient float64  `json:"backoff_coefficient,omitempty"`
	MaxInterval        Duration `json:"max_interval,omitempty"`
	MaxAttempts        int      `json:"max_attempts,omitempty"`
	NonRetryableErrors []string `json:"non_retryable_errors,omitempty"`
}

// Duration 以 "48h"、"30s"、"7d"、"1d12h" 这样的字符串序列化；d 表示 24 小时。
type Duration time.Duration

// ParseDuration 在 time.ParseDuration 的基础上支持 d（天）。
func ParseDuration(s string) (Duration, error) {
	var total time.Duration
	rest := s
	if i := strings.IndexByte(rest, 'd'); i >= 0 {
		days, err := strconv.ParseFloat(rest[:i], 64)
		if err != nil {
			return 0, fmt.Errorf("dsl: invalid duration %q", s)
		}
		total = time.Duration(days * float64(24*time.Hour))
		rest = rest[i+1:]
	}
	if rest != "" {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return 0, fmt.Errorf("dsl: invalid duration %q", s)
		}
		total += d
	}
	if s == "" {
		return 0, fmt.Errorf("dsl: empty duration")
	}
	return Duration(total), nil
}

// Std 返回 time.Duration。
func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) String() string {
	td := time.Duration(d)
	if td >= 24*time.Hour && td%(24*time.Hour) == 0 {
		return strconv.FormatInt(int64(td/(24*time.Hour)), 10) + "d"
	}
	return td.String()
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// ---- 分配策略与卡片模板的引用（节点与任务类型共用） ----

// ViewBinding 引用卡片模板：views 中的名称、共享组件 key@version，或带受众变体的对象。
type ViewBinding struct {
	Default  string        `json:"default"`
	Variants []ViewVariant `json:"variants,omitempty"`
}

// ViewVariant 按查看者选择模板。
type ViewVariant struct {
	When string `json:"when"`
	View string `json:"view"`
}

func (v ViewBinding) MarshalJSON() ([]byte, error) {
	if len(v.Variants) == 0 {
		return json.Marshal(v.Default)
	}
	type plain ViewBinding
	return json.Marshal(plain(v))
}

func (v *ViewBinding) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		*v = ViewBinding{}
		return json.Unmarshal(b, &v.Default)
	}
	type plain ViewBinding
	var p plain
	if err := strictUnmarshal(b, &p); err != nil {
		return err
	}
	*v = ViewBinding(p)
	return nil
}

// Names 返回绑定中引用的全部模板名（默认模板在前）。
func (v ViewBinding) Names() []string {
	out := []string{v.Default}
	for _, x := range v.Variants {
		out = append(out, x.View)
	}
	return out
}

// AssignmentBinding 是内嵌的分配策略，或共享组件 key@version。
type AssignmentBinding struct {
	Ref    string
	Inline *Assignment
}

func (a AssignmentBinding) MarshalJSON() ([]byte, error) {
	if a.Inline != nil {
		return json.Marshal(a.Inline)
	}
	return json.Marshal(a.Ref)
}

func (a *AssignmentBinding) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	*a = AssignmentBinding{}
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &a.Ref)
	}
	a.Inline = &Assignment{}
	return strictUnmarshal(b, a.Inline)
}

// Assignment 是策略链：解析器 → 过滤 → 选择 → 兜底。
type Assignment struct {
	Resolvers []Resolver `json:"resolvers"`
	Filters   []string   `json:"filters,omitempty"`
	Strategy  string     `json:"strategy,omitempty"`
	Exclude   []string   `json:"exclude,omitempty"` // 表达式
	Fallback  *Fallback  `json:"fallback,omitempty"`
}

// Resolver 是候选人解析器。字段按 Type 取用，其余为空。
type Resolver struct {
	Type      string   `json:"type"` // relation / user / group / agreement / previous_assignee / expr
	Relation  string   `json:"relation,omitempty"`
	Subject   string   `json:"subject,omitempty"` // 表达式
	Users     []string `json:"users,omitempty"`
	Group     string   `json:"group,omitempty"`
	Agreement string   `json:"agreement,omitempty"`
	Node      string   `json:"node,omitempty"`
	Expr      string   `json:"expr,omitempty"` // 表达式
}

// Fallback 是所有解析器都没有结果时的兜底。
type Fallback struct {
	Group    string `json:"group"`
	Strategy string `json:"strategy,omitempty"`
}

// ---- 各节点类型的配置 ----

// ConditionConfig：按顺序选中第一个为 true 的分支，都不满足走 default。
type ConditionConfig struct {
	Branches []Branch `json:"branches"`
}

// Branch 是一个条件分支，Port 即输出端口名。
type Branch struct {
	Port  string `json:"port"`
	Label string `json:"label,omitempty"`
	When  string `json:"when"`
}

// MergeMode 是汇合语义，借鉴 Airflow 的触发规则。
type MergeMode string

const (
	// MergeAll：所有入边都有结论且至少一条触发（none_failed_min_one_success）。普通节点有多条入边时也按此处理。
	MergeAll MergeMode = "all"
	// MergeAny：任一入边触发即执行，之后到达的忽略（one_success）。
	MergeAny MergeMode = "any"
)

// MergeConfig 是 merge 节点的配置。
type MergeConfig struct {
	Mode MergeMode `json:"mode,omitempty"`
}

// TimerConfig：等待到 Anchor + After；Anchor 为空时从节点开始执行的时刻算起。
type TimerConfig struct {
	After  Duration `json:"after"`
	Anchor string   `json:"anchor,omitempty"` // 表达式，Unix 毫秒或 RFC3339
}

// HumanChoiceConfig 是 human_choice 节点的配置。
// View / Assignment 省略时继承上一个人工节点，最前面的节点回退到任务类型默认值（编译期解析）。
type HumanChoiceConfig struct {
	View       *ViewBinding       `json:"view,omitempty"`
	Assignment *AssignmentBinding `json:"assignment,omitempty"`
	Options    []Option           `json:"options"`
	Form       *Form              `json:"form,omitempty"`
	Timeout    *Duration          `json:"timeout,omitempty"` // 空 = 一直等待
	Remind     *RemindConfig      `json:"remind,omitempty"`
}

// Option 是卡片上的一个按钮。Snooze 非空时表示推迟，不产生输出端口。
type Option struct {
	Key            string    `json:"key"`
	Label          string    `json:"label"`
	Style          string    `json:"style,omitempty"`
	RequiredFields []string  `json:"required_fields,omitempty"`
	Snooze         *Duration `json:"snooze,omitempty"`
}

// Form 是选择时可填写的表单。
type Form struct {
	Fields []FormField `json:"fields"`
}

// FormField 是表单字段。
type FormField struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Options []string `json:"options,omitempty"`
}

// RemindConfig 是等待期间的重复提醒，与超时相互独立。
type RemindConfig struct {
	Every Duration `json:"every"`
	Max   int      `json:"max"`
}

// SkillCallConfig 调用 Skillhub 或平台内置的 skill。Input 的字符串值是模板。
type SkillCallConfig struct {
	Skill   string         `json:"skill"`
	Version string         `json:"version,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Timeout *Duration      `json:"timeout,omitempty"`
}

// LLMConfig 用提示词模板调用模型。
type LLMConfig struct {
	Prompt    string         `json:"prompt"`
	Inputs    map[string]any `json:"inputs,omitempty"`
	Model     string         `json:"model,omitempty"`
	MaxTokens int            `json:"max_tokens,omitempty"`
	Timeout   *Duration      `json:"timeout,omitempty"`
}

// LarkCardConfig 只通知、不等待。View / To 省略时按与人工节点相同的规则继承。
type LarkCardConfig struct {
	View *ViewBinding       `json:"view,omitempty"`
	To   *AssignmentBinding `json:"to,omitempty"`
}

// SendEmailConfig 发送邮件，字符串均为模板。
type SendEmailConfig struct {
	To      []string `json:"to"`
	CC      []string `json:"cc,omitempty"`
	Subject string   `json:"subject"`
	Body    string   `json:"body,omitempty"`
	View    string   `json:"view,omitempty"`
}

// EmitEventConfig 发出一个平台事件。
type EmitEventConfig struct {
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// EndConfig 结束流程并写入任务结论。
type EndConfig struct {
	Resolution string `json:"resolution,omitempty"`
}

// TriggerConfig 是入口节点，没有配置。
type TriggerConfig struct{}

// DecodeConfig 按节点类型严格解码配置（未知字段报错，数字保留为 json.Number）。
func DecodeConfig(t NodeType, raw json.RawMessage) (any, error) {
	var v any
	switch t {
	case NodeTrigger:
		v = &TriggerConfig{}
	case NodeCondition:
		v = &ConditionConfig{}
	case NodeMerge:
		v = &MergeConfig{}
	case NodeTimer:
		v = &TimerConfig{}
	case NodeHumanChoice:
		v = &HumanChoiceConfig{}
	case NodeSkillCall:
		v = &SkillCallConfig{}
	case NodeLLM:
		v = &LLMConfig{}
	case NodeLarkCard:
		v = &LarkCardConfig{}
	case NodeSendEmail:
		v = &SendEmailConfig{}
	case NodeEmitEvent:
		v = &EmitEventConfig{}
	case NodeEnd:
		v = &EndConfig{}
	default:
		return nil, fmt.Errorf("dsl: unknown node type %q", t)
	}
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return v, nil
	}
	if err := strictUnmarshal(raw, v); err != nil {
		return nil, err
	}
	return v, nil
}

func strictUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	return dec.Decode(v)
}
