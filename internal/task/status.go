// Package task 是任务中心的领域模型：任务状态、优先级、转移规则与服务接口。
//
// 状态与优先级的取值对齐 Morning Brief，设计见 docs/ai-task-platform.md「任务状态」「任务优先级」。
package task

import "fmt"

// TaskStatus 是任务的主状态，只表达生命周期；其余信息放在正交字段里。
// 取值与 Morning Brief 对齐，新增的 TaskStatusUnassigned 取 -1，
// 因此已有的 0~3 不需要重新编号，“>= TaskStatusSolved 即终态”的判断保持成立。
type TaskStatus int16

const (
	TaskStatusUnassigned TaskStatus = -1 // 待分配：组队列中待认领，或等待分配服务确定处理人
	TaskStatusPending    TaskStatus = 0  // 已分配并投递给处理人，等待处理
	TaskStatusProcessing TaskStatus = 1  // 已接手（点过「开始处理」或外链）
	TaskStatusSnoozed    TaskStatus = 2  // 主动推迟，到期回迁 pending
	TaskStatusSolved     TaskStatus = 3  // 已完结；>= 此值即终态
)

// IsTerminal 报告任务是否已处于终态。
func (s TaskStatus) IsTerminal() bool { return s >= TaskStatusSolved }

// Valid 报告取值是否为已定义的状态。
func (s TaskStatus) Valid() bool { return s >= TaskStatusUnassigned && s <= TaskStatusSolved }

func (s TaskStatus) String() string {
	switch s {
	case TaskStatusUnassigned:
		return "unassigned"
	case TaskStatusPending:
		return "pending"
	case TaskStatusProcessing:
		return "processing"
	case TaskStatusSnoozed:
		return "snoozed"
	case TaskStatusSolved:
		return "solved"
	default:
		return fmt.Sprintf("TaskStatus(%d)", int16(s))
	}
}

// TaskPriority 是任务优先级，数值越小越紧急。由业务方在任务类型或订阅中定义，
// 平台只负责求值、存储，并提供 UpdatePriority 接口调整。
type TaskPriority int16

const (
	TaskPriorityP0 TaskPriority = 0 // 立即
	TaskPriorityP1 TaskPriority = 1 // 今日
	TaskPriorityP2 TaskPriority = 2 // 机会
	TaskPriorityP3 TaskPriority = 3 // 维护
)

// Valid 报告取值是否为已定义的优先级。
func (p TaskPriority) Valid() bool { return p >= TaskPriorityP0 && p <= TaskPriorityP3 }

// Resolution 是任务完结时的结果码，与 TaskStatusSolved 搭配使用。
type Resolution string

const (
	ResolutionContacted    Resolution = "contacted"
	ResolutionDismissed    Resolution = "dismissed"
	ResolutionEscalatedOut Resolution = "escalated_out"
	ResolutionAutoClosed   Resolution = "auto_closed" // 源工单离开 maker 状态、run 被取消
)

// SLAStatus 由定时器或扫描更新，不改变主状态。
type SLAStatus string

const (
	SLAOnTrack  SLAStatus = "on_track"
	SLAAtRisk   SLAStatus = "at_risk"
	SLABreached SLAStatus = "breached"
)
