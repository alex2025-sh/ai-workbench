// Package nodes 是节点执行器注册表。新增节点类型 = 注册一个执行器，推进器代码不变。
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
)

// ExecContext 是节点执行时可见的上下文。
type ExecContext struct {
	Run       engine.Run
	Node      dsl.Node
	Iteration int
	Attempt   int
	Inputs    json.RawMessage // 已按变量引用求值的输入
	IdemKey   string          // 外部副作用的幂等键
	DryRun    bool
}

// Executor 是一种节点类型的实现。
type Executor interface {
	Type() dsl.NodeType

	// Ports 返回该节点在给定配置下的输出端口，编译期用于校验连线。
	Ports(cfg json.RawMessage) ([]string, error)

	// Validate 在编译期校验节点配置。
	Validate(cfg json.RawMessage) error

	// Execute 在事务外执行节点，结果交给推进器提交。
	Execute(ctx context.Context, ec ExecContext) engine.NodeResult
}

// Registry 保存所有已注册的节点执行器。
type Registry struct {
	mu        sync.RWMutex
	executors map[dsl.NodeType]Executor
}

// NewRegistry 创建空的注册表。
func NewRegistry() *Registry {
	return &Registry{executors: make(map[dsl.NodeType]Executor)}
}

// Register 注册执行器；同一类型重复注册返回错误。
func (r *Registry) Register(e Executor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.executors[e.Type()]; ok {
		return fmt.Errorf("nodes: executor for %q already registered", e.Type())
	}
	r.executors[e.Type()] = e
	return nil
}

// Get 返回某类型的执行器。
func (r *Registry) Get(t dsl.NodeType) (Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.executors[t]
	return e, ok
}
