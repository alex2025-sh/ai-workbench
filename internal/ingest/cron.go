package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	apischema "github.com/alex2025-sh/ai-workbench/api/schema"
	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
	"github.com/alex2025-sh/ai-workbench/internal/task"
)

// Tasks 是 cron 来源用到的任务中心能力。
type Tasks interface {
	Create(ctx context.Context, cmd task.CreateCmd) (*task.Task, bool, error)
	FindByDedupKey(ctx context.Context, key string) (*task.Task, error) // 不存在时返回 nil, nil
	SetRun(ctx context.Context, id, runID int64) error
	AutoClose(ctx context.Context, id int64, reason string) error // 关闭尚未启动 run 的任务
}

// Runs 是来源对 workflow 的操作：新数据启动 run，外部状态变化取消 run。
type Runs interface {
	StartRun(ctx context.Context, cmd engine.StartRunCmd) (engine.Run, bool, error)
	Cancel(ctx context.Context, runID int64, actor, reason string) error
}

// badRecord 表示记录本身有问题（映射、校验、表达式求值失败）：跳过这条，继续扫描。
// 其余错误（数据库、引擎）视为暂时性失败：停止本次扫描且不推进游标，下次从同一位置重扫；
// 已处理的记录由任务去重键与 run 的 biz_key 保证不会重复。
type badRecord struct{ err error }

func (b badRecord) Error() string { return b.err.Error() }
func (b badRecord) Unwrap() error { return b.err }

func bad(err error) error {
	if err == nil {
		return nil
	}
	return badRecord{err}
}

func isBad(err error) bool {
	var b badRecord
	return errors.As(err, &b)
}

// CronRunner 触发到期的 cron 来源：调用查询 skill 增量扫描，对每条记录：
//   - close_when 为 true：按去重键找到未完结的任务，取消它的 run（外部状态变化推动流程结束）；
//   - filter 为 true：映射为 payload → 按 schema 校验 → 按去重键创建任务 → 启动 workflow。
//
// 由 ticker 调用。
type CronRunner struct {
	Schedules model.WfScheduleModel
	Plans     engine.PlanSource
	Tasks     Tasks
	Runs      Runs
	Actions   engine.Invoker
	Clock     engine.Clock
}

// NewCronRunner 创建 CronRunner。
func NewCronRunner(conn sqlx.SqlConn, plans engine.PlanSource, tasks Tasks, runs Runs, actions engine.Invoker, clock engine.Clock) *CronRunner {
	return &CronRunner{Schedules: model.NewWfScheduleModel(conn), Plans: plans, Tasks: tasks, Runs: runs, Actions: actions, Clock: clock}
}

// FireResult 是一次触发的统计。
type FireResult struct {
	Schedule string
	Scanned  int
	Filtered int
	Invalid  int
	Created  int
	Closed   int
	Err      error
}

// FireDue 触发全部到期的来源。先用乐观锁推进 next_fire_at，只有推进成功的实例执行扫描，
// 多实例同时 tick 也不会重复触发。
func (r *CronRunner) FireDue(ctx context.Context) []FireResult {
	now := r.Clock.Now()
	due, err := r.Schedules.FindDue(ctx, now, 50)
	if err != nil {
		logx.WithContext(ctx).Errorf("ingest: find due schedules: %v", err)
		return nil
	}
	var out []FireResult
	for _, s := range due {
		next, err := registry.NextFire(s.Cron, s.Timezone, now)
		if err != nil {
			out = append(out, FireResult{Schedule: s.Name, Err: err})
			continue
		}
		ok, err := r.Schedules.Advance(ctx, s.Id, s.Version, next)
		if err != nil || !ok {
			continue
		}
		res := r.fire(ctx, s)
		msg := ""
		if res.Err != nil {
			msg = res.Err.Error()
			logx.WithContext(ctx).Errorf("ingest: schedule %s: %v", s.Name, res.Err)
		}
		_ = r.Schedules.SaveResult(ctx, s.Id, res.cursor, msg)
		out = append(out, res.FireResult)
	}
	return out
}

type fireOutcome struct {
	FireResult
	cursor string
}

func (r *CronRunner) fire(ctx context.Context, s *model.WfSchedule) fireOutcome {
	res := fireOutcome{FireResult: FireResult{Schedule: s.Name}, cursor: s.ScanCursor}
	fail := func(err error) fireOutcome { res.Err = err; return res }

	var target registry.ScheduleTarget
	if err := json.Unmarshal([]byte(s.Target), &target); err != nil {
		return fail(fmt.Errorf("bad target: %w", err))
	}
	def, err := r.Plans.Load(ctx, target.TaskType, target.Version)
	if err != nil {
		return fail(err)
	}
	var src *spec.Source
	for i := range def.Spec.Sources {
		if def.Spec.Sources[i].ID == target.SourceID {
			src = &def.Spec.Sources[i]
		}
	}
	if src == nil {
		return fail(fmt.Errorf("source %q not found in %s@%d", target.SourceID, target.TaskType, target.Version))
	}

	input := map[string]any{}
	for k, v := range src.Input {
		input[k] = v
	}
	input["cursor"] = s.ScanCursor
	in, _ := json.Marshal(input)
	cfg, _ := json.Marshal(action.SkillConfig{Skill: src.Skill})
	out, err := r.Actions.InvokeAction(ctx, action.Request{Kind: action.KindSkill, Config: cfg, Input: in,
		IdemKey: fmt.Sprintf("schedule:%d:%d", s.Id, s.Version)})
	if err != nil {
		return fail(err)
	}
	var scanned struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.Unmarshal(out.Output, &scanned); err != nil {
		return fail(fmt.Errorf("skill %s output: %w", src.Skill, err))
	}

	var filter, closeWhen *expr.Program
	if src.Filter != "" {
		if filter, err = expr.Compile(src.Filter); err != nil {
			return fail(err)
		}
	}
	if src.CloseWhen != "" {
		if closeWhen, err = expr.Compile(src.CloseWhen); err != nil {
			return fail(err)
		}
	}
	for i, item := range scanned.Items {
		res.Scanned++
		outcome, err := r.record(ctx, def, src, filter, closeWhen, item, s.Name)
		switch {
		case err == nil:
		case isBad(err):
			// 坏数据不阻塞游标：记录日志并计数，避免一条坏数据卡住整个来源。
			res.Invalid++
			logx.WithContext(ctx).Errorf("ingest: %s record %d skipped: %v", s.Name, i, err)
			continue
		default:
			// 暂时性失败：保留原游标，下次 tick 从同一位置重扫，不丢这条记录。
			return fail(fmt.Errorf("record %d: %w", i, err))
		}
		switch outcome {
		case outcomeClosed:
			res.Closed++
		case outcomeFiltered:
			res.Filtered++
		case outcomeCreated:
			res.Created++
		}
	}
	res.cursor = scanned.NextCursor
	return res
}

type outcome int

const (
	outcomeNone outcome = iota
	outcomeClosed
	outcomeFiltered
	outcomeCreated
)

// record 处理一条扫描记录：先判断 close_when，再判断 filter，最后建任务并启动 run。
func (r *CronRunner) record(ctx context.Context, def *engine.Definition, src *spec.Source, filter, closeWhen *expr.Program, item json.RawMessage, schedule string) (outcome, error) {
	rec, err := expr.Decode(item)
	if err != nil {
		return outcomeNone, bad(err)
	}
	env := expr.Env{expr.RootRecord: rec}
	if closeWhen != nil {
		hit, err := closeWhen.Run(env)
		if err != nil {
			return outcomeNone, bad(fmt.Errorf("close_when: %w", err))
		}
		if b, _ := hit.(bool); b {
			closed, err := r.close(ctx, def, src, env, schedule)
			if err != nil || !closed {
				return outcomeNone, err
			}
			return outcomeClosed, nil
		}
	}
	if filter != nil {
		ok, err := filter.Run(env)
		if err != nil {
			return outcomeNone, bad(fmt.Errorf("filter: %w", err))
		}
		if b, _ := ok.(bool); !b {
			return outcomeFiltered, nil
		}
	}
	created, err := r.ingest(ctx, def, src, env)
	if err != nil || !created {
		return outcomeNone, err
	}
	return outcomeCreated, nil
}

// close 按记录的去重键找到未完结的任务并结束它：有 run 时取消 run，任务由引擎同步为 solved（auto_closed）；
// run 还没启动（启动前失败）时直接关闭任务，否则任务会一直停在待分配。
func (r *CronRunner) close(ctx context.Context, def *engine.Definition, src *spec.Source, env expr.Env, schedule string) (bool, error) {
	payload, body, err := mapRecord(src, env)
	if err != nil {
		return false, bad(err)
	}
	dedup, err := dedupKey(def.Spec, payload, body)
	if err != nil {
		return false, bad(err)
	}
	t, err := r.Tasks.FindByDedupKey(ctx, def.Key+":"+dedup)
	if err != nil || t == nil || t.Status.IsTerminal() {
		return false, err
	}
	reason := "close_when matched in " + schedule
	if t.RunID == 0 {
		return true, r.Tasks.AutoClose(ctx, t.ID, reason)
	}
	return true, r.Runs.Cancel(ctx, t.RunID, "source:"+src.ID, reason)
}

func mapRecord(src *spec.Source, env expr.Env) (map[string]any, []byte, error) {
	payload := map[string]any{}
	for field, e := range src.Mapping {
		p, err := expr.Compile(e)
		if err != nil {
			return nil, nil, err
		}
		v, err := p.Run(env)
		if err != nil {
			return nil, nil, fmt.Errorf("mapping %s: %w", field, err)
		}
		if v != nil {
			payload[field] = v
		}
	}
	body, err := json.Marshal(payload)
	return payload, body, err
}

// ingest 把一条记录映射为 payload，创建任务并启动 workflow。重复数据由任务的去重键拦截；
// 任务已存在但 run 还没启动（上次启动前进程崩溃）时补启动，run 的 biz_key 保证不会重复。
func (r *CronRunner) ingest(ctx context.Context, def *engine.Definition, src *spec.Source, env expr.Env) (bool, error) {
	payload, body, err := mapRecord(src, env)
	if err != nil {
		return false, bad(err)
	}
	vs, err := apischema.ValidatePayload(def.Spec.Schema, body)
	if err != nil {
		return false, bad(err)
	}
	if len(vs) > 0 {
		return false, bad(fmt.Errorf("payload does not match schema: /%s %s", strings.Join(vs[0].Location, "/"), vs[0].Message))
	}
	dedup, err := dedupKey(def.Spec, payload, body)
	if err != nil {
		return false, bad(err)
	}
	t, created, err := r.Tasks.Create(ctx, task.CreateCmd{
		TaskType: def.Key, TypeVersion: def.Version, SubjectType: def.Key, SubjectID: dedup,
		Priority: task.ParsePriority(def.Spec.Priority()), DedupKey: def.Key + ":" + dedup, Payload: body,
	})
	if err != nil {
		return false, err
	}
	if t.RunID != 0 || t.Status.IsTerminal() {
		return created, nil
	}
	// 只提交启动，不就地推进：一次扫描可能启动很多 run，由同一次 tick 随后的 ProcessDue 并行执行。
	run, _, err := r.Runs.StartRun(ctx, engine.StartRunCmd{
		PlanKey: def.Key, PlanVersion: def.Version, BizKey: fmt.Sprintf("task:%d", t.ID), Defer: true,
		Vars: engine.Vars{Payload: body, Task: engine.TaskRef{ID: t.ID, Type: def.Key, Priority: def.Spec.Priority()}},
	})
	if err != nil {
		return created, err
	}
	return created, r.Tasks.SetRun(ctx, t.ID, run.ID)
}

// dedupKey 按任务类型的 dedup_key 模板计算去重键；未配置时用 payload 的哈希。
func dedupKey(t *spec.TaskType, payload map[string]any, body []byte) (string, error) {
	if t.DedupKey == "" {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:16]), nil
	}
	tpl, err := expr.ParseTemplate(t.DedupKey)
	if err != nil {
		return "", err
	}
	key, err := tpl.Render(expr.Env{expr.RootPayload: payload})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("dedup_key rendered empty")
	}
	return key, nil
}
