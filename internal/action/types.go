package action

// 各类动作的请求与结果结构。引擎的节点执行器按这里的结构构造 Request.Config / Input，
// 动作实现按同样的结构解析，双方不需要互相依赖。

// SkillConfig 是 KindSkill 的配置；Input 为渲染后的入参对象。
type SkillConfig struct {
	Skill   string `json:"skill"`
	Version string `json:"version,omitempty"`
}

// AssignInput 是 KindAssign 的入参；Config 为 dsl.AssignmentBinding 的 JSON。
type AssignInput struct {
	Payload any `json:"payload"`
	Task    any `json:"task"`
}

// AssignOutput 是分配结果：要么是具体处理人，要么是待认领的组。
type AssignOutput struct {
	Assignee   string `json:"assignee,omitempty"`
	Group      string `json:"group,omitempty"`
	ResolvedBy string `json:"resolved_by,omitempty"` // 命中的解析器，如 relation:vip_rm / fallback
}

// Card 是渲染后、与渠道无关的卡片内容。脱敏在渲染时已完成。
type Card struct {
	Title    string        `json:"title"`
	Color    string        `json:"color,omitempty"`
	Summary  string        `json:"summary,omitempty"`
	Fields   []CardField   `json:"fields"`
	Links    []CardLink    `json:"links,omitempty"`
	Actions  []CardAction  `json:"actions,omitempty"`
	Callback *CardCallback `json:"callback,omitempty"` // 有按钮时，点击回调定位到 run 的哪个节点、哪一轮
}

// CardField 是卡片中的一个字段。
type CardField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// CardLink 是卡片中的跳转链接。
type CardLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// CardAction 是卡片上的按钮，对应 human_choice 的一个选项。
type CardAction struct {
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	Style          string   `json:"style,omitempty"`
	RequiredFields []string `json:"required_fields,omitempty"`
}

// CardCallback 是按钮点击时回传给 Signal 的定位信息。
type CardCallback struct {
	RunID     int64  `json:"run_id"`
	NodeID    string `json:"node_id"`
	Iteration int    `json:"iteration"`
}

// LarkCardInput 是 KindLarkCard 的入参。
type LarkCardInput struct {
	To   AssignOutput `json:"to"`
	Card Card         `json:"card"`
}

// LarkCardOutput 是发送结果。
type LarkCardOutput struct {
	MessageID string `json:"message_id"`
}

// EmailInput 是 KindSendEmail 的入参。
type EmailInput struct {
	To      []string `json:"to"`
	CC      []string `json:"cc,omitempty"`
	Subject string   `json:"subject"`
	Body    string   `json:"body,omitempty"`
	Card    *Card    `json:"card,omitempty"` // 用卡片模板渲染正文时
}

// EventInput 是 KindEmitEvent 的入参。
type EventInput struct {
	EventType string `json:"event_type"`
	Payload   any    `json:"payload"`
}

// LLMInput 是 KindLLM 的入参。
type LLMInput struct {
	Prompt    string `json:"prompt"`
	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Inputs    any    `json:"inputs"`
}
