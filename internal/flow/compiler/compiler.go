// Package compiler 在发布时校验 DSL 并生成不可变的执行计划（Plan）。
//
// 复杂的结构判断全部放在编译期：环检测、端口校验、变量引用可达性、
// rework 边的支配关系与单入口判定、view / assignment 的继承解析。
// 运行时只读 Plan，不再遍历原始 DSL。
package compiler

import (
	"encoding/json"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
)

// Plan 是编译产物，发布后不可变，运行实例启动时锁定版本。
type Plan struct {
	PlanID  string    `json:"plan_id"`
	Version int       `json:"version"`
	Graph   dsl.Graph `json:"graph"`
	Entry   string    `json:"entry"`

	// Order 是去掉 rework 边后的拓扑序。传播与遍历只使用切片，保证结果稳定、可复现。
	Order []string `json:"order"`

	// In / Out 按节点给出入边与出边，切片顺序在编译期固定。
	In  map[string][]dsl.Edge `json:"in"`
	Out map[string][]dsl.Edge `json:"out"`

	// Loops 是每条 rework 边对应的环区域。
	Loops []LoopRegion `json:"loops,omitempty"`

	// Human 是每个人工节点在编译期解析后的展示模板与分配策略（已处理继承）。
	Human map[string]ResolvedHuman `json:"human,omitempty"`
}

// LoopRegion 是由一条 rework 边围成的单入口区域。
type LoopRegion struct {
	EdgeID        string   `json:"edge_id"`
	Head          string   `json:"head"`  // 回边目标，必须支配回边源
	Nodes         []string `json:"nodes"` // 区域内节点，按拓扑序
	MaxIterations int      `json:"max_iterations"`
	OnExhausted   string   `json:"on_exhausted"`
}

// ResolvedHuman 是人工节点最终使用的配置，以及它来自哪里。
type ResolvedHuman struct {
	View           json.RawMessage `json:"view"`
	ViewFrom       string          `json:"view_from"` // 节点自身 / 上游节点 ID / "task_type"
	Assignment     json.RawMessage `json:"assignment"`
	AssignmentFrom string          `json:"assignment_from"`
}

// IssueCode 是编译问题的分类，供画布高亮与 AI 校验—修复循环使用。
type IssueCode string

const (
	IssueCycle             IssueCode = "cycle"
	IssueUnreachable       IssueCode = "unreachable"
	IssueNoOutgoing        IssueCode = "no_outgoing"
	IssueUnknownPort       IssueCode = "unknown_port"
	IssueMissingTimeout    IssueCode = "missing_timeout_edge"
	IssueBadConfig         IssueCode = "bad_config"
	IssueBadReference      IssueCode = "bad_reference"
	IssueReworkSource      IssueCode = "rework_source"
	IssueReworkNotDominant IssueCode = "rework_not_dominant"
	IssueReworkOverlap     IssueCode = "rework_overlap"
	IssueReworkLimits      IssueCode = "rework_limits"
	IssueInheritConflict   IssueCode = "inherit_conflict"
)

// Issue 是一条编译问题。
type Issue struct {
	Code    IssueCode `json:"code"`
	NodeID  string    `json:"node_id,omitempty"`
	EdgeID  string    `json:"edge_id,omitempty"`
	Message string    `json:"message"`
}

// Defaults 是编译时需要的外部默认值，如任务类型上的 view 与 assignment。
type Defaults struct {
	TaskTypeView       json.RawMessage
	TaskTypeAssignment json.RawMessage
}

// Compiler 校验 DSL 并生成 Plan。存在任何 Issue 时返回的 Plan 为 nil。
type Compiler interface {
	Compile(g dsl.Graph, defaults Defaults) (*Plan, []Issue)
}
