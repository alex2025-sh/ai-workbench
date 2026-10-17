package dsl

import (
	"encoding/json"
	"fmt"
)

// PortRule 说明节点的输出端口从哪里来，前端据此在画布上画出端口。
type PortRule string

const (
	PortsFixed        PortRule = "fixed"         // 固定端口，见 FixedPorts
	PortsFromBranches PortRule = "from_branches" // condition：每个分支一个端口 + default
	PortsFromOptions  PortRule = "from_options"  // human_choice：每个非推迟选项一个端口，配置超时时另有 timeout
)

// Category 是节点类别。
type Category string

const (
	CategoryNative Category = "native"
	CategoryAction Category = "action"
)

// NodeTypeInfo 是节点类型的元数据，用于 CatalogService.ListNodeTypes 与编译期端口校验。
type NodeTypeInfo struct {
	Type        NodeType
	Category    Category
	DisplayName string
	Description string
	PortRule    PortRule
	FixedPorts  []string
}

var actionPorts = []string{PortNext, PortError}

// catalog 按画布面板中的展示顺序排列。
var catalog = []NodeTypeInfo{
	{NodeTrigger, CategoryNative, "开始", "流程入口，接收任务 payload。", PortsFixed, []string{PortNext}},
	{NodeCondition, CategoryNative, "条件分支", "按顺序求值分支条件，选中第一个为 true 的分支，都不满足走 default。", PortsFromBranches, nil},
	{NodeMerge, CategoryNative, "汇合", "并行或条件分支的汇合点，模式 all / any。", PortsFixed, []string{PortNext}},
	{NodeTimer, CategoryNative, "定时等待", "等待一段时间，可以从某个时间字段开始算。", PortsFixed, []string{PortNext}},
	{NodeHumanChoice, CategoryNative, "人工选择", "确定处理人、发送卡片并等待选择，每个选项一个出口。", PortsFromOptions, nil},
	{NodeSkillCall, CategoryAction, "调用 Skill", "调用 Skillhub 或平台内置的 skill。", PortsFixed, actionPorts},
	{NodeLLM, CategoryAction, "LLM", "用提示词模板调用模型。", PortsFixed, actionPorts},
	{NodeLarkCard, CategoryAction, "Lark 通知", "发送 Lark 卡片，只通知、不等待选择。", PortsFixed, actionPorts},
	{NodeSendEmail, CategoryAction, "发送邮件", "按模板发送邮件。", PortsFixed, actionPorts},
	{NodeEmitEvent, CategoryAction, "发出事件", "发出一个平台事件，可触发其他任务类型。", PortsFixed, actionPorts},
	{NodeEnd, CategoryNative, "结束", "结束流程，写入任务的处理结论。", PortsFixed, nil},
}

// NodeTypes 返回全部节点类型，顺序固定。
func NodeTypes() []NodeTypeInfo {
	out := make([]NodeTypeInfo, len(catalog))
	copy(out, catalog)
	return out
}

// LookupNodeType 返回某个节点类型的元数据。
func LookupNodeType(t NodeType) (NodeTypeInfo, bool) {
	for _, c := range catalog {
		if c.Type == t {
			return c, true
		}
	}
	return NodeTypeInfo{}, false
}

// Ports 根据节点类型与配置返回输出端口，顺序固定。
func Ports(n Node) ([]string, error) {
	info, ok := LookupNodeType(n.Type)
	if !ok {
		return nil, fmt.Errorf("dsl: unknown node type %q", n.Type)
	}
	switch info.PortRule {
	case PortsFromBranches:
		cfg, err := DecodeConfig(n.Type, n.Config)
		if err != nil {
			return nil, err
		}
		var ports []string
		for _, b := range cfg.(*ConditionConfig).Branches {
			ports = append(ports, b.Port)
		}
		return append(ports, PortDefault), nil
	case PortsFromOptions:
		cfg, err := DecodeConfig(n.Type, n.Config)
		if err != nil {
			return nil, err
		}
		hc := cfg.(*HumanChoiceConfig)
		var ports []string
		for _, o := range hc.Options {
			if o.Snooze == nil {
				ports = append(ports, o.Key)
			}
		}
		if hc.Timeout != nil {
			ports = append(ports, PortTimeout)
		}
		return ports, nil
	default:
		return append([]string(nil), info.FixedPorts...), nil
	}
}

// Canonical 返回值的规范 JSON，用于比较两个配置是否相同（结构体字段顺序固定，map 键有序）。
func Canonical(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
