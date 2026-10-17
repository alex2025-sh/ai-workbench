// Package registry 是配置中心：组件的草稿、变更集、审批与发布。
//
// 版本模型（见 docs/ai-task-platform.md「版本管理」）：
//   - 任务类型是发布单元。schema、来源、workflow 与专属展示模板是它的自有组件，
//     内嵌在任务类型的版本里，随任务类型一起升版本。
//   - 共享组件（展示模板、分配策略、跨部门协议、prompt）独立版本化，被任务类型以 key@version 固定引用，
//     不支持“跟随最新”。
//   - 每个已发布的任务类型版本都有一份发布清单（manifest），记录解析后的全部依赖与内容哈希，
//     运行时只按清单取配置。
//
// 发布流程：草稿（每个组件一份，revision 乐观锁）→ 变更集（编译依赖闭包）→ 公司审批平台 →
// 审批通过后重新编译并原子发布。已发布版本不可修改；AI 只能写草稿。
package registry

import (
	"context"
	"encoding/json"
)

// Kind 是可独立版本化的组件类型。
type Kind string

const (
	KindTaskType   Kind = "task_type"  // 发布单元，内嵌 schema、来源、workflow 与专属模板
	KindView       Kind = "view"       // 共享展示模板
	KindAssignment Kind = "assignment" // 共享分配策略
	KindAgreement  Kind = "agreement"  // 跨部门协议，由对方部门维护
	KindPrompt     Kind = "prompt"     // 共享 prompt（如 AI 摘要）
)

// VersionState 是已提交版本的生命周期。
type VersionState string

const (
	VersionPublished VersionState = "published"
	VersionRetired   VersionState = "retired"
)

// ChangeSetState 是变更集的状态。
type ChangeSetState string

const (
	ChangeSetOpen      ChangeSetState = "open"
	ChangeSetSubmitted ChangeSetState = "submitted" // 已提交审批，草稿被快照并锁定
	ChangeSetPublished ChangeSetState = "published"
	ChangeSetRejected  ChangeSetState = "rejected"
	ChangeSetWithdrawn ChangeSetState = "withdrawn"
)

// Ref 指向某个组件的某个版本，如 view:vip_ticket_card@3。
type Ref struct {
	Kind    Kind
	Key     string
	Version int
}

// Draft 是组件的草稿。每个组件最多一份，保存时校验 Revision。
type Draft struct {
	Kind        Kind
	Key         string
	BaseVersion int // 草稿基于的已发布版本，0 表示新组件
	Body        json.RawMessage
	Revision    int64
	ChangeSetID int64 // 非 0 表示已被提交中的变更集锁定
}

// ManifestItem 是发布清单的一行：任务类型版本解析后的一个依赖。
type ManifestItem struct {
	ItemKind    string // 如 workflow、view、assignment、agreement、prompt
	ItemKey     string
	Ref         Ref
	ContentHash string
}

// Issue 是编译或校验发现的问题。
type Issue struct {
	Path     string
	Code     string
	Message  string
	Severity string // error 阻止提交，warning 只提示
}

// Registry 管理草稿、变更集与发布。调用者身份从请求上下文获取，不作为参数传入。
type Registry interface {
	GetDraft(ctx context.Context, kind Kind, key string) (*Draft, error)
	// SaveDraft 在 revision 不匹配时返回冲突错误。
	SaveDraft(ctx context.Context, kind Kind, key string, body json.RawMessage, revision int64) (*Draft, error)
	DiscardDraft(ctx context.Context, kind Kind, key string) error

	// CreateChangeSet 把一组草稿纳入变更集，并编译其依赖闭包。
	CreateChangeSet(ctx context.Context, namespace, title string, drafts []Ref) (changeSetID int64, issues []Issue, err error)
	// AddDependentUpgrades 把引用了变更中共享组件的任务类型一并升级到新版本引用。
	AddDependentUpgrades(ctx context.Context, changeSetID int64, taskTypeKeys []string) ([]Issue, error)
	// Submit 快照草稿并发起公司审批平台的审批实例。
	Submit(ctx context.Context, changeSetID int64) (approvalInstanceID string, err error)
	Withdraw(ctx context.Context, changeSetID int64) error
	// OnApproval 处理审批结果：通过则重新编译并原子发布，驳回则解锁草稿。重复回调必须幂等。
	OnApproval(ctx context.Context, approvalInstanceID string, approved bool, approver string) error

	Manifest(ctx context.Context, taskTypeKey string, version int) ([]ManifestItem, error)
	// Dependents 返回引用了某个共享组件版本的任务类型版本。
	Dependents(ctx context.Context, ref Ref) ([]Ref, error)
	Retire(ctx context.Context, ref Ref) error
}
