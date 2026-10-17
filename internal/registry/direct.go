package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

// ScheduleTarget 是 wf_schedule.target 的内容：触发哪个任务类型版本的哪个 cron 来源。
type ScheduleTarget struct {
	TaskType string `json:"task_type"`
	Version  int    `json:"version"`
	SourceID string `json:"source_id"`
}

// ScheduleName 是 cron 来源在 wf_schedule 中的唯一名称。
func ScheduleName(taskType, sourceID string) string { return taskType + ":" + sourceID }

// DirectPublishResult 是直接发布的结果。
type DirectPublishResult struct {
	Key     string
	Version int
	Issues  []compiler.Issue
}

// PublishDirect 跳过草稿、变更集与审批，直接把一份任务类型 JSON 发布为新版本：
// 写入 wf_plan，并把 cron 来源登记到 wf_schedule（已存在的同名来源更新为新版本，保留扫描游标）。
//
// 只用于本地开发与一期的 YAML 发布；正式发布走变更集与公司审批平台（见 Registry）。
// 存在 error 级别的问题时不发布，问题在结果中返回。
func PublishDirect(ctx context.Context, conn sqlx.SqlConn, body []byte, publishedBy string, now time.Time) (*DirectPublishResult, error) {
	res := spec.Check(body)
	out := &DirectPublishResult{Issues: res.Issues}
	if !res.OK() {
		return out, nil
	}
	t := res.Spec
	out.Key = t.Key
	compiled, err := json.Marshal(res.Plan)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	err = conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		plans := model.NewWfPlanModel(conn).WithSession(session)
		max, err := plans.MaxVersion(ctx, t.Key)
		if err != nil {
			return err
		}
		out.Version = int(max) + 1
		if _, err := plans.Insert(ctx, &model.WfPlan{
			PlanId: t.Key, Version: int64(out.Version), Dsl: string(body), Compiled: string(compiled),
			Checksum: hex.EncodeToString(sum[:]), PublishedBy: publishedBy, PublishedAt: now,
		}); err != nil {
			return err
		}
		schedules := model.NewWfScheduleModel(conn).WithSession(session)
		for _, src := range t.Sources {
			if src.Kind != spec.SourceCron {
				continue
			}
			if err := upsertSchedule(ctx, schedules, t.Key, out.Version, src, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// NextFire 计算 cron 表达式在 after 之后的下一次触发时间（UTC）。
func NextFire(expr, timezone string, after time.Time) (time.Time, error) {
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	loc := time.UTC
	if timezone != "" {
		if loc, err = time.LoadLocation(timezone); err != nil {
			return time.Time{}, err
		}
	}
	return sched.Next(after.In(loc)).UTC(), nil
}

func upsertSchedule(ctx context.Context, m model.WfScheduleModel, key string, version int, src spec.Source, now time.Time) error {
	tz := src.Timezone
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	next, err := NextFire(src.Schedule, tz, now)
	if err != nil {
		return fmt.Errorf("registry: source %s: %w", src.ID, err)
	}
	target, _ := json.Marshal(ScheduleTarget{TaskType: key, Version: version, SourceID: src.ID})
	name := ScheduleName(key, src.ID)
	existing, err := m.FindOneByName(ctx, name)
	switch {
	case errors.Is(err, model.ErrNotFound):
		_, err = m.Insert(ctx, &model.WfSchedule{Name: name, Cron: src.Schedule, Timezone: tz, Target: string(target), NextFireAt: next, Enabled: 1})
		return err
	case err != nil:
		return err
	default:
		existing.Cron, existing.Timezone, existing.Target, existing.NextFireAt, existing.Enabled = src.Schedule, tz, string(target), next, 1
		existing.Version++
		return m.Update(ctx, existing)
	}
}
