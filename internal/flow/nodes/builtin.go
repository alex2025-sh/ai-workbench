package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

func permanent(format string, args ...any) engine.NodeResult {
	return engine.NodeResult{Err: fmt.Errorf(format, args...), Permanent: true}
}

func output(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

// trigger 是入口，在 StartRun 时已直接完成；这里只为完整性注册。
type trigger struct{}

func (trigger) Type() dsl.NodeType { return dsl.NodeTrigger }
func (trigger) Execute(context.Context, engine.ExecContext) engine.NodeResult {
	return engine.NodeResult{Port: dsl.PortNext}
}

// end 结束流程，输出配置的处理结论。
type end struct{}

func (end) Type() dsl.NodeType { return dsl.NodeEnd }
func (end) Execute(_ context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.EndConfig)
	return engine.NodeResult{Output: output(map[string]string{"resolution": cfg.Resolution})}
}

// merge 的汇合语义由推进器在传播时处理，执行时直接走 next。
type merge struct{}

func (merge) Type() dsl.NodeType { return dsl.NodeMerge }
func (merge) Execute(context.Context, engine.ExecContext) engine.NodeResult {
	return engine.NodeResult{Port: dsl.PortNext}
}

// condition 按顺序求值分支，选中第一个为 true 的分支；都不满足走 default。
type condition struct{}

func (condition) Type() dsl.NodeType { return dsl.NodeCondition }
func (condition) Execute(_ context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.ConditionConfig)
	for _, b := range cfg.Branches {
		p, err := expr.Compile(b.When)
		if err != nil {
			return permanent("branch %s: %v", b.Port, err)
		}
		v, err := p.Run(ec.Env)
		if err != nil {
			return permanent("branch %s: %v", b.Port, err)
		}
		ok, isBool := v.(bool)
		if !isBool {
			return permanent("branch %s: condition %q returned %T, want bool", b.Port, b.When, v)
		}
		if ok {
			return engine.NodeResult{Port: b.Port, Output: output(map[string]string{"branch": b.Port})}
		}
	}
	return engine.NodeResult{Port: dsl.PortDefault, Output: output(map[string]string{"branch": dsl.PortDefault})}
}

// timer 等待到 anchor + after；anchor 为空时从执行时刻算起。到期时间已过的立即触发。
type timer struct{}

func (timer) Type() dsl.NodeType { return dsl.NodeTimer }
func (timer) Execute(_ context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.TimerConfig)
	base := ec.Now
	if cfg.Anchor != "" {
		p, err := expr.Compile(cfg.Anchor)
		if err != nil {
			return permanent("anchor: %v", err)
		}
		v, err := p.Run(ec.Env)
		if err != nil {
			return permanent("anchor: %v", err)
		}
		t, err := toTime(v)
		if err != nil {
			return permanent("anchor %q: %v", cfg.Anchor, err)
		}
		base = t
	}
	due := base.Add(cfg.After.Std()).UTC().Truncate(time.Millisecond)
	return engine.NodeResult{
		Wait:   &engine.WaitSpec{TimerAt: &due},
		Output: output(map[string]any{"due_at": due}),
	}
}

// toTime 把 Unix 毫秒或 RFC3339 字符串转为时间。
func toTime(v any) (time.Time, error) {
	switch x := v.(type) {
	case int64:
		return time.UnixMilli(x).UTC(), nil
	case int:
		return time.UnixMilli(int64(x)).UTC(), nil
	case float64:
		return time.UnixMilli(int64(x)).UTC(), nil
	case string:
		return time.Parse(time.RFC3339, x)
	case nil:
		return time.Time{}, fmt.Errorf("value is empty")
	default:
		return time.Time{}, fmt.Errorf("unsupported time value %T", v)
	}
}
