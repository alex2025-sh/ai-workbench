// Package dsl 定义 workflow 的图模型：节点 + 端口 + 连线。
//
// 画布与 AI 生成的是同一份 DSL；DSL 只描述结构与配置，不包含脚本。
// 设计见 docs/workflow-engine.md「DSL 与编译器」「受控的环」。
package dsl

import (
	"encoding/json"
	"time"
)

// NodeType 是节点类型。新增节点类型 = 在 nodes 注册表中注册一个执行器。
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

// 通用端口名。各节点类型还会声明自己的端口（如 human_choice 的每个选项、condition 的每个分支）。
const (
	PortNext    = "next"
	PortError   = "error"
	PortTimeout = "timeout"
	PortDefault = "default"
)

// JoinMode 是多条入边时的汇合语义，借鉴 Airflow 的触发规则。
type JoinMode string

const (
	// JoinAll：所有入边都有结论且至少一条触发（none_failed_min_one_success）。普通节点的默认值。
	JoinAll JoinMode = "all"
	// JoinAny：任一入边触发即执行，之后到达的忽略（one_success）。
	JoinAny JoinMode = "any"
)

// EdgeKind 区分普通边与受控的回边。
type EdgeKind string

const (
	EdgeNormal EdgeKind = ""       // 普通边：必须构成 DAG
	EdgeRework EdgeKind = "rework" // 退回边：显式声明，源节点须为 human_choice
)

// Graph 是一个 workflow 定义的完整图。
type Graph struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Name    string `json:"name"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
}

// Node 是图中的一个节点。Config 由节点类型自己的 JSON Schema 校验。
type Node struct {
	ID          string          `json:"id"`
	Type        NodeType        `json:"type"`
	Name        string          `json:"name,omitempty"`
	Join        JoinMode        `json:"join,omitempty"`
	Config      json.RawMessage `json:"config,omitempty"`
	Retry       *RetryPolicy    `json:"retry,omitempty"`
	SideEffects []SideEffect    `json:"side_effects,omitempty"`
}

// Edge 从某节点的某端口连到另一节点。
type Edge struct {
	ID       string   `json:"id"`
	From     string   `json:"from"`
	FromPort string   `json:"port"`
	To       string   `json:"to"`
	Kind     EdgeKind `json:"kind,omitempty"`

	// 以下仅对 rework 边有效，且为必填。
	MaxIterations int    `json:"max_iterations,omitempty"`
	OnExhausted   string `json:"on_exhausted,omitempty"` // 超过次数后的去向节点
}

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

// SideEffect 是挂在节点上的旁路动作，如通知其他系统、发邮件、AI 摘要。
type SideEffect struct {
	Action    string              `json:"action"`
	Config    json.RawMessage     `json:"config,omitempty"`
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

// Duration 以 Go duration 字符串（如 "48h"、"30s"）序列化。
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// HumanChoiceConfig 是 human_choice 节点的配置。
// View / Assignment 省略时继承上一个人工节点，最前面的节点回退到任务类型默认值（编译期解析）。
type HumanChoiceConfig struct {
	Assignment json.RawMessage     `json:"assignment,omitempty"`
	View       json.RawMessage     `json:"view,omitempty"`
	Options    []string            `json:"options"`
	Form       map[string][]string `json:"form,omitempty"`    // option -> 必填字段
	Timeout    string              `json:"timeout,omitempty"` // 固定时长或表达式；空 = 一直等待
	Remind     *RemindConfig       `json:"remind,omitempty"`
}

// RemindConfig 是等待期间的重复提醒，与超时相互独立。
type RemindConfig struct {
	Every Duration `json:"every"`
	Max   int      `json:"max"`
}
