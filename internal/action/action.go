// Package action 是动作运行时：所有外部 IO（skill、LLM、Lark、邮件、事件）都经 InvokeAction 分发。
//
// 引擎核心只知道“调用一个动作”，新增动作只需在这里注册实现。
// 动作是至少执行一次的，实现方必须用 Request.IdemKey 在下游去重。
package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Kind 是动作类型。
type Kind string

const (
	KindSkill     Kind = "skill"     // Skillhub / MCP：查询、推理、动作三类 skill
	KindLLM       Kind = "llm"       // 经 Eino 调用 LiteLLM
	KindLarkCard  Kind = "lark_card" // 发送或更新 Lark 卡片
	KindSendEmail Kind = "send_email"
	KindEmitEvent Kind = "emit_event" // 发布领域事件，旁路通知其他系统
	KindAssign    Kind = "assign"     // 调用分配服务，为人工节点确定处理人
	KindSyncTask  Kind = "sync_task"  // 把节点事件投影到任务中心
)

// Request 是一次动作调用。
type Request struct {
	Kind    Kind
	Config  json.RawMessage
	Input   json.RawMessage
	IdemKey string // run_id:node_id:iteration[:seq]，随请求传给下游
	DryRun  bool   // 试运行：只记录“发给谁、发什么”，不真正调用
}

// Result 是动作调用的结果。
type Result struct {
	Output   json.RawMessage
	Retrying bool // true 表示可重试的失败
}

// Error 是带错误码的动作错误。节点的重试策略按错误码匹配 non_retryable_errors，
// 错误码也会写进节点输出，供 error 端口之后的节点判断。
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return "action: " + e.Code
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// ErrorCode 实现错误码接口。
func (e *Error) ErrorCode() string { return e.Code }

// 平台内置的错误码；动作实现可以用 *Error 返回自己的错误码。
const (
	CodeTimeout      = "timeout"       // 超过节点的单次执行超时
	CodeUnknownSkill = "unknown_skill" // skill 不存在
)

// ErrorCode 返回错误的错误码：错误链上实现了 ErrorCode() 的取其值，超时为 timeout，其余为空。
func ErrorCode(err error) string {
	var c interface{ ErrorCode() string }
	if errors.As(err, &c) {
		return c.ErrorCode()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeTimeout
	}
	return ""
}

// Action 是一种动作的实现。
type Action interface {
	Kind() Kind
	Invoke(ctx context.Context, req Request) (Result, error)
}

// Runtime 按 Kind 分发动作调用。
type Runtime struct {
	mu      sync.RWMutex
	actions map[Kind]Action
}

// NewRuntime 创建空的动作运行时。
func NewRuntime() *Runtime { return &Runtime{actions: make(map[Kind]Action)} }

// Register 注册一个动作实现。
func (r *Runtime) Register(a Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actions[a.Kind()]; ok {
		return fmt.Errorf("action: %q already registered", a.Kind())
	}
	r.actions[a.Kind()] = a
	return nil
}

// InvokeAction 分发一次动作调用。
func (r *Runtime) InvokeAction(ctx context.Context, req Request) (Result, error) {
	r.mu.RLock()
	a, ok := r.actions[req.Kind]
	r.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("action: no implementation for %q", req.Kind)
	}
	return a.Invoke(ctx, req)
}
