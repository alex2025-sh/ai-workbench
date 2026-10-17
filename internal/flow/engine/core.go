package engine

import (
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
)

// state 是传播计算使用的节点状态，只包含推导边状态需要的字段。
type state struct {
	Status NodeStatus
	Port   string
}

// edgeState 是由源节点推导出的边状态。
type edgeState int

const (
	edgeUndecided edgeState = iota
	edgeFired
	edgeSkipped
)

func edgeOf(st map[string]*state, e dsl.Edge) edgeState {
	src := st[e.From]
	switch {
	case src == nil:
		return edgeUndecided
	case src.Status == NodeSkipped:
		return edgeSkipped
	case src.Status == NodeSucceeded && src.Port == e.Port:
		return edgeFired
	case src.Status == NodeSucceeded:
		return edgeSkipped
	default:
		return edgeUndecided
	}
}

// mergeModes 预先算出 merge 节点的模式；其余节点按 all 处理。
func mergeModes(p *compiler.Plan) map[string]dsl.MergeMode {
	out := map[string]dsl.MergeMode{}
	for _, n := range p.Graph.Nodes {
		if n.Type != dsl.NodeMerge {
			continue
		}
		mode := dsl.MergeAll
		if cfg, err := dsl.DecodeConfig(n.Type, n.Config); err == nil && cfg.(*dsl.MergeConfig).Mode != "" {
			mode = cfg.(*dsl.MergeConfig).Mode
		}
		out[n.ID] = mode
	}
	return out
}

// propagate 在节点 from 得出结论（成功或跳过）后沿普通出边传播，就地修改 st：
//   - 目标节点的入边全部有结论且至少一条触发 → ready（any 模式下任一触发即 ready）；
//   - 全部跳过 → skipped，并继续向下传播（死路径消除）；
//   - 还有未决入边 → 保持 pending。
//
// 只处理 pending 的目标，保证每个节点在一轮中只被调度一次。遍历 Plan 中排好序的切片，结果可复现。
// 返回按发生顺序排列的新 ready 与新 skipped 节点。
func propagate(p *compiler.Plan, modes map[string]dsl.MergeMode, st map[string]*state, from string) (ready, skipped []string) {
	queue := []string{from}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range p.Out[n] {
			if e.IsRework() {
				continue
			}
			t := st[e.To]
			if t == nil || t.Status != NodePending {
				continue
			}
			fired, undecided := 0, 0
			for _, in := range p.In[e.To] {
				switch edgeOf(st, in) {
				case edgeFired:
					fired++
				case edgeUndecided:
					undecided++
				}
			}
			switch {
			case modes[e.To] == dsl.MergeAny && fired > 0:
				t.Status = NodeReady
				ready = append(ready, e.To)
			case undecided > 0:
				// 等待其他入边
			case fired > 0:
				t.Status = NodeReady
				ready = append(ready, e.To)
			default:
				t.Status = NodeSkipped
				skipped = append(skipped, e.To)
				queue = append(queue, e.To)
			}
		}
	}
	return ready, skipped
}

// allConcluded 报告是否所有节点都已成功或跳过。
func allConcluded(p *compiler.Plan, st map[string]*state) bool {
	for _, id := range p.Order {
		if s := st[id]; s == nil || !s.Status.Concluded() {
			return false
		}
	}
	return true
}
