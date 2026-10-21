// Package tasktype 是任务类型的配置模型（前后端契约）：数据源 + 业务字段 + 人工节点链 + 卡片。
//
// 结构契约见 api/schema/task_type.schema.json，字段说明见 docs/task-type-spec.md。
// 校验分两层：JSON Schema 管结构（类型、必填、取值范围），Validate 管语义（引用、顺序、唯一性）。
package tasktype

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/alex2025-sh/ai-workbench/internal/pkg/yamljson"
)

// TaskType 是一个任务类型。
type TaskType struct {
	Key         string  `json:"key"`
	Namespace   string  `json:"namespace,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Priority    string  `json:"priority,omitempty"` // P0..P3，默认 P2
	Source      Source  `json:"source"`
	Fields      []Field `json:"fields"`
	Card        Card    `json:"card"`
	Nodes       []Node  `json:"nodes"`
}

// 数据源类型。
const (
	SourceSQL   = "sql"
	SourceAPI   = "api"
	SourceKafka = "kafka"
)

// Source 是任务的来源。SQL 来源由平台按 cron 执行查询；API / Kafka 由业务方按平台协议推送。
// 三种来源的语义相同：某个 key 当前「在」或「不在」待办集合里。
type Source struct {
	Kind       string `json:"kind"`
	Datasource string `json:"datasource,omitempty"` // sql：平台注册的只读连接名
	Schedule   string `json:"schedule,omitempty"`   // sql：5 段 cron
	Timezone   string `json:"timezone,omitempty"`   // sql：cron 的时区，默认 Asia/Shanghai
	Query      string `json:"query,omitempty"`      // sql：单条 SELECT，必须有一列叫 key，结果集就是当前待办
}

// 字段类型。
const (
	FieldText     = "text"
	FieldNumber   = "number"
	FieldMoney    = "money"    // 字符串承载，不经过 float64
	FieldDatetime = "datetime" // Unix 秒、毫秒或 RFC3339
	FieldEnum     = "enum"
)

// 脱敏方式：发往 Lark 与日志时，以及工作台展示时都生效。
const (
	PIIMaskMiddle = "mask_middle"
	PIIHash       = "hash"
	PIIDrop       = "drop"
)

// Field 是一个业务字段。SQL 来源的字段名就是查询结果的列名。
type Field struct {
	Name    string       `json:"name"`
	Label   string       `json:"label,omitempty"` // 缺省时显示字段名
	Type    string       `json:"type,omitempty"`  // 缺省为 text
	PII     string       `json:"pii,omitempty"`
	Options []EnumOption `json:"options,omitempty"` // enum：取值与显示名
}

// EnumOption 是枚举字段的一个取值。
type EnumOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Card 是任务卡片：标题模板 + KV 列表。工作台、任务详情、Lark 用同一份渲染结果。
// 模板只支持字段占位 {字段名}，{key} 是数据源给出的 key。
type Card struct {
	Title  string   `json:"title"`
	Fields []string `json:"fields,omitempty"` // 字段名，顺序即显示顺序
}

// 节点上可启用的动作。
const (
	ActionClose      = "close"       // 已处理，任务完结
	ActionNoResponse = "no_response" // 无需响应，任务完结
	ActionSnooze     = "snooze"      // 推迟，到期回到待处理并重新通知
	ActionLink       = "link"        // 打开业务系统链接，不改变状态
	ActionEscalate   = "escalate"    // 移到下一个节点
)

// Actions 是全部动作，按显示顺序排列。
var Actions = []string{ActionClose, ActionNoResponse, ActionSnooze, ActionLink, ActionEscalate}

// Node 是一个人工节点。节点按数组顺序组成升级链，escalate 移到下一个。
type Node struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Assignee Assignee `json:"assignee"`
	Actions  []string `json:"actions"`
	Link     *Link    `json:"link,omitempty"`   // 启用 link 时必填
	Snooze   []string `json:"snooze,omitempty"` // 可选的推迟时长，缺省为 DefaultSnooze
	Lark     bool     `json:"lark,omitempty"`   // 进入节点、分配到人、推迟到期时发 Lark 通知
}

// DefaultSnooze 是节点没有配置推迟时长时的选项。
var DefaultSnooze = []string{"1h", "4h", "1d"}

// SnoozeOptions 返回节点可选的推迟时长。
func (n Node) SnoozeOptions() []string {
	if len(n.Snooze) > 0 {
		return n.Snooze
	}
	return DefaultSnooze
}

// Has 报告节点是否启用了某个动作。
func (n Node) Has(action string) bool {
	for _, a := range n.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// 分配器类型：任务进入节点时总是直接分配给一个人。
const (
	AssigneeRelation = "relation" // 按业务关系找到一个人（如 VIP → RM），查不到时交给兜底人
	AssigneePool     = "pool"     // 从一批人中按策略选一个
)

// 人员池的选择策略。
const (
	StrategyRandom = "random"
)

// Assignee 是节点的分配器。
type Assignee struct {
	Kind     string   `json:"kind"`
	Relation string   `json:"relation,omitempty"` // relation：关系名，如 vip_rm
	Field    string   `json:"field,omitempty"`    // relation：主体字段，如 uid
	Fallback string   `json:"fallback,omitempty"` // relation：查不到时的兜底人
	Users    []string `json:"users,omitempty"`    // pool：候选人
	Strategy string   `json:"strategy,omitempty"` // pool：选择策略，缺省 random
}

// Link 是 redirect link 动作的按钮。
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"` // 可用字段占位
}

// Node 按 id 返回节点及其下标。
func (t *TaskType) Node(id string) (Node, int, bool) {
	for i, n := range t.Nodes {
		if n.ID == id {
			return n, i, true
		}
	}
	return Node{}, -1, false
}

// Next 返回 id 之后的节点。
func (t *TaskType) Next(id string) (Node, bool) {
	_, i, ok := t.Node(id)
	if !ok || i+1 >= len(t.Nodes) {
		return Node{}, false
	}
	return t.Nodes[i+1], true
}

// Field 按名字返回字段定义。
func (t *TaskType) Field(name string) (Field, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// PriorityValue 返回默认优先级的数值（P0 = 0），缺省为 2。
func (t *TaskType) PriorityValue() int {
	switch t.Priority {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P3":
		return 3
	default:
		return 2
	}
}

// Parse 解析任务类型 JSON（未知字段报错）。只做解码，校验见 Validate。
func Parse(raw []byte) (*TaskType, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var t TaskType
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("tasktype: %w", err)
	}
	return &t, nil
}

// FromYAML 把 YAML（YAML 1.2 语义）转换为任务类型 JSON。
func FromYAML(raw []byte) ([]byte, error) { return yamljson.Convert(raw) }
