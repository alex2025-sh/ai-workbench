package task

import (
	"encoding/json"
	"time"
)

// Resolution 是任务完结时的结果。
type Resolution string

const (
	ResolutionHandled    Resolution = "handled"     // close：已处理
	ResolutionNoResponse Resolution = "no_response" // no_response：无需响应 / 不再跟进
	ResolutionAutoClosed Resolution = "auto_closed" // 数据源里的 key 消失
)

// Task 是任务。任务行就是流程状态的唯一事实来源。
type Task struct {
	ID          int64
	TaskType    string
	TypeVersion int
	BizKey      string
	Tracked     bool            // 跟踪中：key 仍在数据源结果里时不重复创建（含手工完结的任务）
	Fields      json.RawMessage // 业务字段
	NodeID      string
	Status      TaskStatus
	Priority    TaskPriority
	AssigneeID  string
	SnoozeUntil *time.Time
	Resolution  Resolution
	SourceAt    int64
	Version     int64
	ClosedAt    *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
