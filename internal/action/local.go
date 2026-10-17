package action

import (
	"context"
	"encoding/json"

	"github.com/zeromicro/go-zero/core/logx"
)

// Logger 是只打日志的动作实现，用于尚未接入的外部系统（邮件、事件总线、LLM）。
type Logger struct {
	K Kind
	// Reply 是返回给节点的输出；为空时返回 {}。
	Reply json.RawMessage
}

// Kind 实现 Action。
func (l Logger) Kind() Kind { return l.K }

// Invoke 实现 Action。
func (l Logger) Invoke(ctx context.Context, req Request) (Result, error) {
	logx.WithContext(ctx).Infof("%s(mock) idem=%s input=%s", l.K, req.IdemKey, string(req.Input))
	out := l.Reply
	if len(out) == 0 {
		out = json.RawMessage(`{}`)
	}
	return Result{Output: out}, nil
}

// SkillFunc 是一个平台内置 skill。
type SkillFunc func(ctx context.Context, input json.RawMessage) (any, error)

// Skills 是 KindSkill 的实现：先查平台内置 skill，找不到时交给 Fallback（如 Skillhub 客户端）。
type Skills struct {
	Builtin  map[string]SkillFunc
	Fallback Action
}

// Kind 实现 Action。
func (*Skills) Kind() Kind { return KindSkill }

// Invoke 实现 Action。
func (s *Skills) Invoke(ctx context.Context, req Request) (Result, error) {
	var cfg SkillConfig
	if err := json.Unmarshal(req.Config, &cfg); err != nil {
		return Result{}, err
	}
	if fn, ok := s.Builtin[cfg.Skill]; ok {
		out, err := fn(ctx, req.Input)
		if err != nil {
			return Result{}, err
		}
		b, err := json.Marshal(out)
		return Result{Output: b}, err
	}
	if s.Fallback != nil {
		return s.Fallback.Invoke(ctx, req)
	}
	return Result{}, &UnknownSkillError{Name: cfg.Skill}
}

// UnknownSkillError 表示 skill 不存在。
type UnknownSkillError struct{ Name string }

func (e *UnknownSkillError) Error() string { return "action: unknown skill " + e.Name }
