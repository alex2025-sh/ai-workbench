package task

import (
	"context"
	"encoding/json"
	"time"
)

// Task 是任务实例的领域视图。持久化由 internal/model 中 goctl 生成的模型负责。
type Task struct {
	ID              int64
	TaskType        string
	TypeVersion     int
	SubjectType     string
	SubjectID       string
	Status          TaskStatus
	Priority        TaskPriority
	AssigneeID      string
	AssigneeGroup   string
	CurrentNode     string
	RunID           int64
	Resolution      Resolution
	SLAStatus       SLAStatus
	EscalationLevel int
	SnoozeUntil     *time.Time
	DueAt           *time.Time
	DedupKey        string
	Payload         json.RawMessage // 平台层不解析，按 task_type@type_version 的 schema 校验
	Seq             int64           // 引擎同步序号
	Version         int64           // 乐观锁
}

// SyncStatusCmd 由引擎的 sync_task job 发出。任务中心只接受 Seq 大于当前值的写入，
// 重复或乱序到达的请求直接忽略，从而实现幂等与防乱序。
type SyncStatusCmd struct {
	TaskID      int64
	Status      TaskStatus
	AssigneeID  string
	CurrentNode string
	Resolution  Resolution
	SnoozeUntil *time.Time
	Seq         int64
}

// Service 是任务中心对外的能力。所有状态变更都经过它：校验转移表、乐观锁、写 Task Event。
//
// 写入入口约定（见 docs/workflow-engine.md「与任务中心的集成」）：
//   - 影响流程的操作（选择、推迟、退回）走引擎 Signal，由引擎经 SyncStatus 回写；
//   - 不影响流程的操作（开始处理、认领、调整优先级、转派）直接调用本接口。
type Service interface {
	Get(ctx context.Context, id int64) (*Task, error)

	// SyncStatus 应用引擎同步过来的状态；Seq 不大于当前值时返回 applied=false 且不报错。
	SyncStatus(ctx context.Context, cmd SyncStatusCmd) (applied bool, err error)

	// Claim 认领组任务：unassigned -> pending，用 CAS 保证只有一人成功。
	Claim(ctx context.Context, id int64, actor string) (*Task, error)

	// StartProcessing 开始处理：pending -> processing，不通知引擎。
	StartProcessing(ctx context.Context, id int64, actor string) (*Task, error)

	// UpdatePriority 调整优先级：需权限，原因必填，写 Task Event，并刷新已发出的卡片。
	UpdatePriority(ctx context.Context, id int64, p TaskPriority, actor, reason string) (*Task, error)

	// Reassign 转派：只改变处理人，状态不变，原因必填。
	Reassign(ctx context.Context, id int64, assignee, actor, reason string) (*Task, error)

	// AutoClose 源工单离开 Maker Review Case 时调用：任务置为 solved(auto_closed)，并取消对应 run。
	AutoClose(ctx context.Context, id int64, reason string) error
}
