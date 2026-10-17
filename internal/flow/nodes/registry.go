// Package nodes 是节点执行器：每种节点类型一个实现，注册到 Registry，交给引擎按类型查找。
//
// 执行器在事务外运行，只通过 engine.ExecContext.Actions（internal/action）访问外部系统，
// 不直接依赖任务中心、分配服务或 Lark；端口与配置校验在编译期由 dsl 包完成。
package nodes

import (
	"fmt"
	"sync"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
)

// Registry 保存所有已注册的节点执行器，实现 engine.Executors。
type Registry struct {
	mu        sync.RWMutex
	executors map[dsl.NodeType]engine.Executor
}

// NewRegistry 创建空的注册表。
func NewRegistry() *Registry {
	return &Registry{executors: make(map[dsl.NodeType]engine.Executor)}
}

// Default 返回注册了全部内置节点类型的注册表。
func Default() *Registry {
	r := NewRegistry()
	for _, e := range []engine.Executor{
		trigger{}, end{}, merge{}, condition{}, timer{},
		skillCall{}, llm{}, larkCard{}, sendEmail{}, emitEvent{}, humanChoice{},
	} {
		if err := r.Register(e); err != nil {
			panic(err)
		}
	}
	return r
}

// Register 注册执行器；同一类型重复注册返回错误。
func (r *Registry) Register(e engine.Executor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.executors[e.Type()]; ok {
		return fmt.Errorf("nodes: executor for %q already registered", e.Type())
	}
	r.executors[e.Type()] = e
	return nil
}

// Get 实现 engine.Executors。
func (r *Registry) Get(t dsl.NodeType) (engine.Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.executors[t]
	return e, ok
}
