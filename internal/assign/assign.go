// Package assign 是分配服务：候选人解析 → 过滤 → 选择策略 → 兜底。
//
// 每个人工节点只由一个人决策；指派给用户组时，组内第一个认领的人成为处理人。
// 设计见 docs/ai-task-platform.md「节点级的展示模板与处理人分配」。
package assign

import (
	"context"
	"encoding/json"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
)

// Policy 是一条分配策略，结构与 DSL 中的 assignment 相同（见 api/schema/task_type.schema.json）。
type Policy = dsl.Assignment

// ResolverType 是候选人解析器的类型。
type ResolverType string

const (
	ResolverRelation         ResolverType = "relation"          // 业务关系，如 VIP → 负责 RM（来自关系 API）
	ResolverUser             ResolverType = "user"              // 固定指定的人
	ResolverGroup            ResolverType = "group"             // 用户组队列
	ResolverAgreement        ResolverType = "agreement"         // 跨部门协议：受理组、响应 SLA、升级联系人
	ResolverPreviousAssignee ResolverType = "previous_assignee" // 固定为上一人工节点的实际处理人
	ResolverExpr             ResolverType = "expr"              // 表达式求值得到 email
)

// Strategy 是从候选人中选出处理人的策略。
type Strategy string

const (
	StrategyFirst      Strategy = "first"
	StrategyClaim      Strategy = "claim" // 进入组队列，由组内成员认领（CAS）
	StrategyRoundRobin Strategy = "round_robin"
	StrategyLeastLoad  Strategy = "least_load"
)

// 过滤器名称。职责分离（not_maker 等）由平台强制执行。
const (
	FilterActive         = "active"
	FilterNotOnLeave     = "not_on_leave"
	FilterNotMaker       = "not_maker"
	FilterWithinCapacity = "within_capacity"
)

// Subject 是被分配的任务主体信息。
type Subject struct {
	TaskID      int64
	TaskType    string
	SubjectType string
	SubjectID   string
	Payload     json.RawMessage
	Exclude     []string // 已求值的排除名单
}

// Result 是一次分配的结果：要么是具体处理人，要么进入组队列待认领。
type Result struct {
	AssigneeID string
	Group      string
	ResolvedBy ResolverType // 记录命中的解析器，写入 Task Event
}

// Service 对一个任务求值分配策略。实现必须是幂等的：重复调用不产生副作用。
type Service interface {
	Resolve(ctx context.Context, p Policy, s Subject) (Result, error)
}

// RelationSource 提供业务关系，如 RM-VIP（一个 RM 对应多个 VIP 或一组 VIP）。
// 实现应带缓存；关系 API 不可用时使用最近一次结果，再不行则走兜底。
type RelationSource interface {
	Lookup(ctx context.Context, relation, subjectID string) ([]string, error)
}
