// Package approval 对接公司现有的审批平台。
//
// 平台只负责发起审批与接收结果，变更集的状态机由 internal/registry 维护。
// 审批结果经 gateway 原样透传（raw body + headers）到 ChangeSetService.OnApprovalResult，
// 在这里验签并解析。
package approval

import (
	"context"
	"net/http"
)

// Request 是一次审批的内容。
type Request struct {
	BizKey    string // 幂等键，使用变更集 ID
	Title     string
	Summary   string // 变更摘要与 diff 链接
	Applicant string
	Approvers []string // 为空时由审批平台按流程配置决定
}

// Result 是审批平台回调的结果。
type Result struct {
	InstanceID string
	BizKey     string
	Approved   bool
	Approver   string
	Comment    string
}

// Client 是审批平台客户端。
type Client interface {
	Create(ctx context.Context, req Request) (instanceID string, err error)
	Cancel(ctx context.Context, instanceID string) error
	// ParseCallback 校验签名并解析回调。
	ParseCallback(ctx context.Context, rawBody []byte, headers http.Header) (*Result, error)
}
