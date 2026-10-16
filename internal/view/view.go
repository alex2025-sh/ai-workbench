// Package view 定义与渠道无关的展示模板，并渲染到 Lark 卡片、Web 工作台与邮件。
//
// 解析顺序：节点上的受众变体 → 节点模板 → 上一个人工节点的模板 → 任务类型默认模板（编译期确定）。
// 卡片按钮由节点的 options 生成，模板只负责样式。
package view

import (
	"context"
	"encoding/json"
	"time"
)

// Channel 是触达渠道。
type Channel string

const (
	ChannelLark  Channel = "lark"
	ChannelWeb   Channel = "web"
	ChannelEmail Channel = "email"
)

// Template 是一个版本化的展示模板。
type Template struct {
	ID        string     `json:"id"`
	Version   int        `json:"version"`
	Header    Header     `json:"header"`
	Fields    []Field    `json:"fields"`
	AISummary *AISummary `json:"ai_summary,omitempty"`
}

// Header 是卡片头部。值支持 {{...}} 表达式。
type Header struct {
	Title string `json:"title"`
	Color string `json:"color,omitempty"`
}

// Field 是卡片中的一个字段。脱敏与格式化由 schema 的 x-pii / x-format 自动处理。
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Variant 按受众选择不同模板，如 viewer.dept == 'aml'。
type Variant struct {
	When string `json:"when"`
	View string `json:"view"` // 模板引用，如 compliance_review_card_aml@1
}

// NodeView 是人工节点上的 view 配置。
type NodeView struct {
	Default  string    `json:"default"`
	Variants []Variant `json:"variants,omitempty"`
}

// AISummary 是可选的 AI 摘要。摘要作为异步旁路动作生成：卡片先发、成功后更新；
// 失败（超时、报错、为空、超出 MaxChars）时整块不显示，只记入事件与指标。
// 输出为自然语言，格式与语气在提示词模板中约定；workflow 的判断逻辑从不读取摘要。
type AISummary struct {
	Enabled  bool     `json:"enabled"`
	Prompt   string   `json:"prompt"` // 提示词模板引用，版本化管理
	Inputs   []string `json:"inputs"` // task payload 字段；字段含义取自 schema 的 title / description
	MaxChars int      `json:"max_chars"`
	Timeout  string   `json:"timeout"`
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
