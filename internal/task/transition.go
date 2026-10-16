package task

import "fmt"

// transitions 是任务状态的合法转移表，对应 docs/ai-task-platform.md 中的状态流转图。
// 转派只改变处理人、不改变状态，因此不在表中。
var transitions = map[TaskStatus][]TaskStatus{
	TaskStatusUnassigned: {TaskStatusPending, TaskStatusSolved},
	TaskStatusPending:    {TaskStatusProcessing, TaskStatusSnoozed, TaskStatusSolved, TaskStatusUnassigned},
	TaskStatusProcessing: {TaskStatusPending, TaskStatusSnoozed, TaskStatusSolved, TaskStatusUnassigned},
	TaskStatusSnoozed:    {TaskStatusPending, TaskStatusSolved},
	TaskStatusSolved:     nil, // 终态
}

// CanTransition 报告 from -> to 是否为合法转移。
func CanTransition(from, to TaskStatus) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// ErrInvalidTransition 表示请求的状态转移不在转移表中。
type ErrInvalidTransition struct {
	From, To TaskStatus
}

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("task: invalid status transition %s -> %s", e.From, e.To)
}

// CheckTransition 在转移非法时返回 ErrInvalidTransition。
func CheckTransition(from, to TaskStatus) error {
	if !CanTransition(from, to) {
		return ErrInvalidTransition{From: from, To: to}
	}
	return nil
}
