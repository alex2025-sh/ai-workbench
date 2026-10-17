package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	SetRun(ctx context.Context, id, runID int64) error
}

// Runs 启动 workflow。
type Runs interface {
	StartRun(ctx context.Context, cmd engine.StartRunCmd) (engine.Run, bool, error)
}

// CronRunner 触发到期的 cron 来源：调用查询 skill 增量扫描 → 过滤 → 映射为 payload → 按 schema 校验
// → 按去重键创建任务 → 启动 workflow。由 ticker 调用。
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

	var filter *expr.Program
	if src.Filter != "" {
		if filter, err = expr.Compile(src.Filter); err != nil {
			return fail(err)
		}
	}
	for _, item := range scanned.Items {
		res.Scanned++
		rec, err := expr.Decode(item)
		if err != nil {
			res.Invalid++
			continue
		}
		env := expr.Env{expr.RootRecord: rec}
		if filter != nil {
			ok, err := filter.Run(env)
			if err != nil {
				res.Invalid++
				continue
			}
			if b, _ := ok.(bool); !b {
				res.Filtered++
				continue
			}
		}
		created, err := r.ingest(ctx, def, src, env)
		if err != nil {
			// 单条失败不阻塞游标：记录日志并计数，避免一条坏数据卡住整个来源。
			res.Invalid++
			logx.WithContext(ctx).Errorf("ingest: %s record: %v", s.Name, err)
			continue
		}
		if created {
			res.Created++
		}
	}
	res.cursor = scanned.NextCursor
	return res
}

// ingest 把一条记录映射为 payload，创建任务并启动 workflow。重复数据由任务的去重键拦截；
// 任务已存在但 run 还没启动（上次启动前进程崩溃）时补启动，run 的 biz_key 保证不会重复。
func (r *CronRunner) ingest(ctx context.Context, def *engine.Definition, src *spec.Source, env expr.Env) (bool, error) {
	payload := map[string]any{}
	for field, e := range src.Mapping {
		p, err := expr.Compile(e)
		if err != nil {
			return false, err
		}
		v, err := p.Run(env)
		if err != nil {
			return false, fmt.Errorf("mapping %s: %w", field, err)
		}
		if v != nil {
			payload[field] = v
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	vs, err := apischema.ValidatePayload(def.Spec.Schema, body)
	if err != nil {
		return false, err
	}
	if len(vs) > 0 {
		return false, fmt.Errorf("payload does not match schema: /%s %s", strings.Join(vs[0].Location, "/"), vs[0].Message)
	}
	dedup, err := dedupKey(def.Spec, payload, body)
	if err != nil {
		return false, err
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
	run, _, err := r.Runs.StartRun(ctx, engine.StartRunCmd{
		PlanKey: def.Key, PlanVersion: def.Version, BizKey: fmt.Sprintf("task:%d", t.ID),
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
