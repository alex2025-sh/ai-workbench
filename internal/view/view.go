// Package view 定义与渠道无关的展示模板，并渲染到 Lark 卡片、Web 工作台与邮件。
//
// 解析顺序：节点上的受众变体 → 节点模板 → 上一个人工节点的模板 → 任务类型默认模板（编译期确定，
// 引用方式见 dsl.ViewBinding）。
// 卡片按钮由节点的 options 生成，模板只负责样式。
package view

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
)

// Channel 是触达渠道。
type Channel string

const (
	ChannelLark  Channel = "lark"
	ChannelWeb   Channel = "web"
	ChannelEmail Channel = "email"
)

// Template 是一个卡片模板，结构即 DSL 中的 view_template（见 api/schema/task_type.schema.json）。
// 自有模板内嵌在任务类型里；共享模板是独立版本的组件，按 key@version 引用。
type Template struct {
	Title     string     `json:"title"`
	Color     string     `json:"color,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Fields    []Field    `json:"fields"`
	Links     []Link     `json:"links,omitempty"`
	AISummary *AISummary `json:"ai_summary,omitempty"`
}

// Field 是卡片中的一个字段。Value 是模板；脱敏与格式化由 schema 的 x-pii / x-format 自动处理。
type Field struct {
	Label string `json:"label,omitempty"` // 缺省时取 schema 中字段的 title
	Value string `json:"value"`
	Short *bool  `json:"short,omitempty"`
}

// Link 是卡片底部的跳转链接。
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// AISummary 是可选的 AI 摘要。摘要作为异步旁路动作生成：卡片先发、成功后更新；
// 失败（超时、报错、为空、超出 MaxChars）时整块不显示，只记入事件与指标。
// 输出为自然语言，格式与语气在提示词模板中约定；workflow 的判断逻辑从不读取摘要。
type AISummary struct {
	Enabled  bool          `json:"enabled"`
	Prompt   string        `json:"prompt,omitempty"` // 提示词模板引用，版本化管理
	Inputs   []string      `json:"inputs,omitempty"` // task payload 字段；字段含义取自 schema 的 title / description
	MaxChars int           `json:"max_chars,omitempty"`
	Timeout  *dsl.Duration `json:"timeout,omitempty"`
}

// Viewer 是查看者信息，用于选择受众变体与脱敏级别。
type Viewer struct {
	UserID string
	Dept   string
	Roles  []string
}

// Action 是卡片上的一个按钮，绑定到人工节点的一个端口。
type Action struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Style     string   `json:"style,omitempty"`
	FormField []string `json:"form,omitempty"`
}

// RenderInput 是一次渲染所需的全部输入。
type RenderInput struct {
	Template  Template
	Payload   json.RawMessage
	SchemaRef string // task_type@version，用于脱敏与格式化
	Viewer    Viewer
	Actions   []Action
	RunID     int64
	NodeID    string
	Iteration int
	Summary   string // 已生成的 AI 摘要；为空则不显示该区块
	Now       time.Time
}

// Renderer 把模板渲染为某个渠道的内容（Lark 卡片为 JSON）。
type Renderer interface {
	Channel() Channel
	Render(ctx context.Context, in RenderInput) (json.RawMessage, error)
}
