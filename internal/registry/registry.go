// Package registry 是配置中心：所有配置的版本、审批与发布。
//
// 配置发布后不可修改，只能发布新版本；AI 只能创建草稿，发布需要人工审批（maker-checker），
// 试运行是发布前的必经步骤。
package registry

import (
	"context"
	"encoding/json"
)

// Kind 是配置类型。
type Kind string

const (
	KindEventType    Kind = "event_type"
	KindTrigger      Kind = "trigger"
	KindSubscription Kind = "subscription"
	KindTaskType     Kind = "task_type"
	KindWorkflow     Kind = "workflow"
	KindView         Kind = "view"
	KindAssignment   Kind = "assignment"
	KindAgreement    Kind = "agreement" // 跨部门协议，由对方部门维护
)

// State 是配置版本的生命周期。
type State string

const (
	StateDraft     State = "draft"
	StatePending   State = "pending_review"
	StatePublished State = "published"
	StateRetired   State = "retired"
)

// Ref 指向某个配置的某个版本，如 vip_ticket_alert@2。
type Ref struct {
	Kind    Kind
	ID      string
	Version int
}

// Entry 是一条版本化配置。
type Entry struct {
	Ref       Ref
	State     State
	Body      json.RawMessage
	CreatedBy string // 人或 AI（如 ai:openclaw）
	Reviewer  string
}

// Registry 管理配置的草稿、审批与发布。
type Registry interface {
	SaveDraft(ctx context.Context, kind Kind, id string, body json.RawMessage, actor string) (Ref, error)
	Submit(ctx context.Context, ref Ref, actor string) error
	Approve(ctx context.Context, ref Ref, reviewer string) error // 审批人不能是起草人
	Get(ctx context.Context, ref Ref) (*Entry, error)
	Latest(ctx context.Context, kind Kind, id string) (*Entry, error)
}
