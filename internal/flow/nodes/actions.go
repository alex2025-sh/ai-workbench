package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/schema"
	"github.com/alex2025-sh/ai-workbench/internal/view"
)

// invoke 调用一个动作并把结果解码到 out（可为 nil）。动作返回错误时按可重试处理。
func invoke(ctx context.Context, ec engine.ExecContext, kind action.Kind, cfg, input any, suffix string, out any) error {
	req := action.Request{Kind: kind, IdemKey: ec.IdemKey + suffix, DryRun: ec.Run.DryRun}
	if cfg != nil {
		req.Config = output(cfg)
	}
	if input != nil {
		req.Input = output(input)
	}
	res, err := ec.Actions.InvokeAction(ctx, req)
	if err != nil {
		return err
	}
	if out != nil && len(res.Output) > 0 {
		return json.Unmarshal(res.Output, out)
	}
	return nil
}

// render 递归渲染：字符串按模板求值，对象与数组逐项渲染，其他值原样返回。
func render(v any, env expr.Env) (any, error) {
	switch x := v.(type) {
	case string:
		if !strings.Contains(x, "{{") {
			return x, nil
		}
		t, err := expr.ParseTemplate(x)
		if err != nil {
			return nil, err
		}
		return t.Eval(env)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			r, err := render(val, env)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			r, err := render(val, env)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

func renderString(s string, env expr.Env) (string, error) {
	t, err := expr.ParseTemplate(s)
	if err != nil {
		return "", err
	}
	return t.Render(env)
}

// maskedEnv 返回出口使用的环境：payload 按 schema 的 x-pii 脱敏。
func maskedEnv(ec engine.ExecContext) expr.Env { return maskEnv(ec.Def, ec.Env) }

func maskEnv(def *engine.Definition, src expr.Env) expr.Env {
	env := expr.Env{}
	for k, v := range src {
		env[k] = v
	}
	if p, ok := src[expr.RootPayload].(map[string]any); ok {
		env[expr.RootPayload] = schema.Mask(def.Spec.Schema, p)
	}
	return env
}

// ---- 卡片 ----

var singleField = regexp.MustCompile(`^\{\{\s*payload\.([A-Za-z0-9_]+)\s*\}\}$`)

// Template 解析卡片模板名：本地模板取任务类型的 views；共享组件 key@version 暂未接入组件库。
// 发送卡片与 Web 任务详情共用同一套解析。
func Template(def *engine.Definition, b *dsl.ViewBinding) (*view.Template, error) {
	if b == nil {
		return nil, fmt.Errorf("no view configured")
	}
	name := b.Default // 发送时不知道查看者，受众变体在 Web 工作台按查看者选择
	if strings.Contains(name, "@") {
		return nil, fmt.Errorf("shared view %q is not supported yet", name)
	}
	v, ok := def.Spec.Views[name]
	if !ok || v.Inline == nil {
		if ok && v.Ref != "" {
			return nil, fmt.Errorf("shared view %q is not supported yet", v.Ref)
		}
		return nil, fmt.Errorf("view %q not found", name)
	}
	return v.Inline, nil
}

// RenderCard 把模板渲染成与渠道无关的卡片：payload 先按 schema 的 x-pii 脱敏，字段缺省标签取 schema 中的 title。
// 发送卡片与 Web 任务详情共用，env 为引擎构造的表达式环境。
func RenderCard(def *engine.Definition, tpl *view.Template, src expr.Env) (action.Card, error) {
	env := maskEnv(def, src)
	titles := fieldTitles(def.Spec.Schema)
	var c action.Card
	var err error
	if c.Title, err = renderString(tpl.Title, env); err != nil {
		return c, fmt.Errorf("title: %w", err)
	}
	if c.Summary, err = renderString(tpl.Summary, env); err != nil {
		return c, fmt.Errorf("summary: %w", err)
	}
	c.Color = tpl.Color
	for i, f := range tpl.Fields {
		val, err := renderString(f.Value, env)
		if err != nil {
			return c, fmt.Errorf("fields.%d: %w", i, err)
		}
		label := f.Label
		if label == "" {
			if m := singleField.FindStringSubmatch(f.Value); m != nil {
				label = titles[m[1]]
			}
		}
		short := f.Short == nil || *f.Short
		c.Fields = append(c.Fields, action.CardField{Label: label, Value: val, Short: short})
	}
	for i, l := range tpl.Links {
		u, err := renderString(l.URL, env)
		if err != nil {
			return c, fmt.Errorf("links.%d: %w", i, err)
		}
		c.Links = append(c.Links, action.CardLink{Label: l.Label, URL: u})
	}
	return c, nil
}

func fieldTitles(raw json.RawMessage) map[string]string {
	var s struct {
		Properties map[string]struct {
			Title string `json:"title"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(raw, &s)
	out := map[string]string{}
	for k, v := range s.Properties {
		out[k] = v.Title
	}
	return out
}

// resolve 调用分配服务确定接收人或处理人。
func resolve(ctx context.Context, ec engine.ExecContext, b *dsl.AssignmentBinding) (action.AssignOutput, error) {
	var out action.AssignOutput
	if b == nil {
		return out, fmt.Errorf("no assignment configured")
	}
	err := invoke(ctx, ec, action.KindAssign, b,
		action.AssignInput{Payload: ec.Env[expr.RootPayload], Task: ec.Env[expr.RootTask]}, ":assign", &out)
	if err == nil && out.Assignee == "" && out.Group == "" {
		err = fmt.Errorf("assignment resolved to nobody")
	}
	return out, err
}

func assignment(a action.AssignOutput) *engine.Assignment {
	return &engine.Assignment{Assignee: a.Assignee, Group: a.Group}
}

// ---- 动作节点 ----

type skillCall struct{}

func (skillCall) Type() dsl.NodeType { return dsl.NodeSkillCall }
func (skillCall) Execute(ctx context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.SkillCallConfig)
	input, err := render(map[string]any(cfg.Input), ec.Env)
	if err != nil {
		return permanent("input: %v", err)
	}
	var out json.RawMessage
	if err := invoke(ctx, ec, action.KindSkill, action.SkillConfig{Skill: cfg.Skill, Version: cfg.Version}, input, "", &out); err != nil {
		return engine.NodeResult{Err: err}
	}
	return engine.NodeResult{Port: dsl.PortNext, Output: out}
}

type llm struct{}

func (llm) Type() dsl.NodeType { return dsl.NodeLLM }
func (llm) Execute(ctx context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.LLMConfig)
	inputs, err := render(map[string]any(cfg.Inputs), maskedEnv(ec))
	if err != nil {
		return permanent("inputs: %v", err)
	}
	var out json.RawMessage
	in := action.LLMInput{Prompt: cfg.Prompt, Model: cfg.Model, MaxTokens: cfg.MaxTokens, Inputs: inputs}
	if err := invoke(ctx, ec, action.KindLLM, nil, in, "", &out); err != nil {
		return engine.NodeResult{Err: err}
	}
	return engine.NodeResult{Port: dsl.PortNext, Output: out}
}

// larkCard 只通知：确定接收人 → 渲染卡片 → 发送。接收人同时成为任务的处理人。
type larkCard struct{}

func (larkCard) Type() dsl.NodeType { return dsl.NodeLarkCard }
func (larkCard) Execute(ctx context.Context, ec engine.ExecContext) engine.NodeResult {
	if ec.Binding == nil {
		return permanent("no resolved view / recipient")
	}
	tpl, err := Template(ec.Def, ec.Binding.View)
	if err != nil {
		return permanent("%v", err)
	}
	card, err := RenderCard(ec.Def, tpl, ec.Env)
	if err != nil {
		return permanent("render: %v", err)
	}
	to, err := resolve(ctx, ec, ec.Binding.Assignment)
	if err != nil {
		return engine.NodeResult{Err: err}
	}
	var sent action.LarkCardOutput
	if err := invoke(ctx, ec, action.KindLarkCard, nil, action.LarkCardInput{To: to, Card: card}, ":card", &sent); err != nil {
		return engine.NodeResult{Err: err}
	}
	return engine.NodeResult{
		Port: dsl.PortNext,
		// 输出带上发出的卡片：Web 任务详情显示的就是这张卡片，不按之后变化的环境重新渲染。
		Output:     output(map[string]any{"to": to, "message_id": sent.MessageID, "card": card}),
		Assignment: assignment(to),
	}
}

// humanChoice 确定处理人 → 渲染带按钮的卡片 → 进入等待；选择经 Engine.Signal 提交，超时走 timeout 端口。
// 卡片作为 effect 返回：引擎先提交 waiting，再发送卡片，处理人点击时节点一定已在等待。
type humanChoice struct{}

func (humanChoice) Type() dsl.NodeType { return dsl.NodeHumanChoice }
func (humanChoice) Execute(ctx context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.HumanChoiceConfig)
	if ec.Binding == nil {
		return permanent("no resolved view / assignment")
	}
	tpl, err := Template(ec.Def, ec.Binding.View)
	if err != nil {
		return permanent("%v", err)
	}
	card, err := RenderCard(ec.Def, tpl, ec.Env)
	if err != nil {
		return permanent("render: %v", err)
	}
	for _, o := range cfg.Options {
		card.Actions = append(card.Actions, action.CardAction{Key: o.Key, Label: o.Label, Style: o.Style, RequiredFields: o.RequiredFields})
	}
	card.Callback = &action.CardCallback{RunID: ec.Run.ID, NodeID: ec.Node.ID, Iteration: ec.Iteration}
	who, err := resolve(ctx, ec, ec.Binding.Assignment)
	if err != nil {
		return engine.NodeResult{Err: err}
	}
	wait := &engine.WaitSpec{}
	if cfg.Timeout != nil {
		at := ec.Now.Add(cfg.Timeout.Std()) // 从进入等待算起，与卡片何时送达无关
		wait.TimeoutAt = &at
	}
	return engine.NodeResult{
		Wait:       wait,
		Output:     output(map[string]any{"assignee": who.Assignee, "group": who.Group, "card": card}),
		Assignment: assignment(who),
		Effects: []engine.Effect{{
			Kind: action.KindLarkCard, Input: output(action.LarkCardInput{To: who, Card: card}), Key: ":card", WhileWaiting: true,
		}},
	}
}

type sendEmail struct{}

func (sendEmail) Type() dsl.NodeType { return dsl.NodeSendEmail }
func (sendEmail) Execute(ctx context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.SendEmailConfig)
	env := maskedEnv(ec)
	in := action.EmailInput{}
	var err error
	for _, t := range cfg.To {
		s, err := renderString(t, env)
		if err != nil {
			return permanent("to: %v", err)
		}
		in.To = append(in.To, s)
	}
	for _, t := range cfg.CC {
		s, err := renderString(t, env)
		if err != nil {
			return permanent("cc: %v", err)
		}
		in.CC = append(in.CC, s)
	}
	if in.Subject, err = renderString(cfg.Subject, env); err != nil {
		return permanent("subject: %v", err)
	}
	if cfg.View != "" {
		tpl, err := Template(ec.Def, &dsl.ViewBinding{Default: cfg.View})
		if err != nil {
			return permanent("%v", err)
		}
		card, err := RenderCard(ec.Def, tpl, ec.Env)
		if err != nil {
			return permanent("render: %v", err)
		}
		in.Card = &card
	} else if in.Body, err = renderString(cfg.Body, env); err != nil {
		return permanent("body: %v", err)
	}
	if err := invoke(ctx, ec, action.KindSendEmail, nil, in, "", nil); err != nil {
		return engine.NodeResult{Err: err}
	}
	return engine.NodeResult{Port: dsl.PortNext, Output: output(map[string]any{"to": in.To})}
}

type emitEvent struct{}

func (emitEvent) Type() dsl.NodeType { return dsl.NodeEmitEvent }
func (emitEvent) Execute(ctx context.Context, ec engine.ExecContext) engine.NodeResult {
	cfg := ec.Config.(*dsl.EmitEventConfig)
	payload, err := render(map[string]any(cfg.Payload), ec.Env)
	if err != nil {
		return permanent("payload: %v", err)
	}
	if err := invoke(ctx, ec, action.KindEmitEvent, nil, action.EventInput{EventType: cfg.EventType, Payload: payload}, "", nil); err != nil {
		return engine.NodeResult{Err: err}
	}
	return engine.NodeResult{Port: dsl.PortNext}
}
