package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/model"
)

// ErrNotImplemented 表示该能力会在任务中心的 RPC 阶段实现。
var ErrNotImplemented = errors.New("task: not implemented yet")

// ErrHasRun 表示任务已有 run，应由引擎取消 run 来结束任务。
var ErrHasRun = errors.New("task: task is driven by a run; cancel the run instead")

// CreateCmd 创建任务。DedupKey 相同的任务只会创建一次。
type CreateCmd struct {
	TaskType    string
	TypeVersion int
	SubjectType string
	SubjectID   string
	Priority    TaskPriority
	DedupKey    string
	Payload     json.RawMessage
}

// Store 是任务中心基于 MySQL / TiDB 的实现。
type Store struct {
	conn   sqlx.SqlConn
	tasks  model.TaskModel
	events model.TaskEventModel
}

var _ Service = (*Store)(nil)

// NewStore 创建 Store。
func NewStore(conn sqlx.SqlConn) *Store {
	return &Store{conn: conn, tasks: model.NewTaskModel(conn), events: model.NewTaskEventModel(conn)}
}

// Create 按去重键创建任务，初始状态为待分配。已存在时返回已有任务与 created=false。
func (s *Store) Create(ctx context.Context, cmd CreateCmd) (*Task, bool, error) {
	if existing, err := s.tasks.FindOneByDedupKey(ctx, cmd.DedupKey); err == nil {
		t := fromRow(existing)
		return &t, false, nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, false, err
	}
	row := &model.Task{
		TaskType: cmd.TaskType, TypeVersion: int64(cmd.TypeVersion), SubjectType: cmd.SubjectType, SubjectId: cmd.SubjectID,
		Status: int64(TaskStatusUnassigned), Priority: int64(cmd.Priority), DedupKey: cmd.DedupKey, Payload: string(cmd.Payload),
		SlaStatus: string(SLAOnTrack),
	}
	var created *Task
	err := s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		res, err := s.tasks.WithSession(session).Insert(ctx, row)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		row.Id = uint64(id)
		t := fromRow(row)
		created = &t
		return s.event(ctx, s.events.WithSession(session), row.Id, "created", nil, TaskStatusUnassigned, "engine", "", nil)
	})
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			existing, ferr := s.tasks.FindOneByDedupKey(ctx, cmd.DedupKey)
			if ferr == nil {
				t := fromRow(existing)
				return &t, false, nil
			}
		}
		return nil, false, err
	}
	return created, true, nil
}

// FindByDedupKey 按去重键查找任务，不存在时返回 nil, nil。
func (s *Store) FindByDedupKey(ctx context.Context, key string) (*Task, error) {
	row, err := s.tasks.FindOneByDedupKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := fromRow(row)
	return &t, nil
}

// AutoClose 关闭尚未启动 run 的任务（启动前失败，之后来源发现源工单已经离开待处理状态）：
// 置为 solved(auto_closed)。已有 run 的任务应由引擎取消 run 后同步，这里不处理，返回 ErrHasRun。
func (s *Store) AutoClose(ctx context.Context, id int64, reason string) error {
	return s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		tm := s.tasks.WithSession(session)
		cur, err := tm.FindOneForUpdate(ctx, uint64(id))
		if err != nil {
			return err
		}
		from := TaskStatus(cur.Status)
		if from.IsTerminal() {
			return nil
		}
		if cur.RunId != 0 {
			return ErrHasRun
		}
		if err := tm.CloseUnstarted(ctx, cur.Id, int64(TaskStatusSolved), string(ResolutionAutoClosed)); err != nil {
			return err
		}
		return s.event(ctx, s.events.WithSession(session), cur.Id, "status_changed", &from, TaskStatusSolved, "source", reason,
			map[string]any{"resolution": ResolutionAutoClosed})
	})
}

// SetRun 记录驱动任务的 run。
func (s *Store) SetRun(ctx context.Context, id, runID int64) error {
	return s.tasks.SetRun(ctx, uint64(id), uint64(runID))
}

// Get 实现 Service。
func (s *Store) Get(ctx context.Context, id int64) (*Task, error) {
	row, err := s.tasks.FindOne(ctx, uint64(id))
	if err != nil {
		return nil, err
	}
	t := fromRow(row)
	return &t, nil
}

// SyncStatus 实现 Service：只接受更大的 seq；任务已终结时忽略。
//
// 处理人的归属：只有流程明确分配处理人时（Reassign）才覆盖；否则保留任务中心当前的处理人，
// 认领、转派的结果不会被引擎里的旧快照覆盖。处理人未变时不把 processing 退回 pending（处理人已点过「开始处理」）。
func (s *Store) SyncStatus(ctx context.Context, cmd SyncStatusCmd) (bool, error) {
	applied := false
	err := s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		tm := s.tasks.WithSession(session)
		cur, err := tm.FindOneForUpdate(ctx, uint64(cmd.TaskID))
		if err != nil {
			return err
		}
		from := TaskStatus(cur.Status)
		if cur.Seq >= cmd.Seq || from.IsTerminal() {
			return nil
		}
		assignee, group := cur.AssigneeId, cur.AssigneeGroup
		if cmd.Reassign {
			assignee, group = cmd.AssigneeID, ""
			if assignee == "" {
				group = cmd.Group
			}
		}
		to := cmd.Status
		if to == TaskStatusPending && from == TaskStatusProcessing && assignee == cur.AssigneeId {
			to = TaskStatusProcessing
		}
		var snooze sql.NullTime
		if cmd.SnoozeUntil != nil {
			snooze = sql.NullTime{Time: *cmd.SnoozeUntil, Valid: true}
		}
		ok, err := tm.ApplySync(ctx, cur.Id, int64(to), assignee, group, cmd.CurrentNode, string(cmd.Resolution), snooze, cmd.Seq)
		if err != nil || !ok {
			return err
		}
		applied = true
		detail := map[string]any{"seq": cmd.Seq, "assignee": assignee, "group": group, "node": cmd.CurrentNode, "resolution": cmd.Resolution}
		typ := "status_changed"
		if from == to && assignee != cur.AssigneeId {
			typ = "reassigned"
		}
		return s.event(ctx, s.events.WithSession(session), cur.Id, typ, &from, to, "engine", "", detail)
	})
	return applied, err
}

func (s *Store) event(ctx context.Context, m model.TaskEventModel, taskID uint64, typ string, from *TaskStatus, to TaskStatus, actor, reason string, detail any) error {
	ev := &model.TaskEvent{TaskId: taskID, Type: typ, ToStatus: sql.NullInt64{Int64: int64(to), Valid: true}, Actor: actor, Reason: reason}
	if from != nil {
		ev.FromStatus = sql.NullInt64{Int64: int64(*from), Valid: true}
	}
	if detail != nil {
		b, _ := json.Marshal(detail)
		ev.Detail = sql.NullString{String: string(b), Valid: true}
	}
	_, err := m.Insert(ctx, ev)
	return err
}

// 以下能力在任务中心 RPC 阶段实现。

func (s *Store) Claim(context.Context, int64, string) (*Task, error) { return nil, ErrNotImplemented }
func (s *Store) StartProcessing(context.Context, int64, string) (*Task, error) {
	return nil, ErrNotImplemented
}
func (s *Store) UpdatePriority(context.Context, int64, TaskPriority, string, string) (*Task, error) {
	return nil, ErrNotImplemented
}
func (s *Store) Reassign(context.Context, int64, string, string, string) (*Task, error) {
	return nil, ErrNotImplemented
}

func fromRow(r *model.Task) Task {
	t := Task{
		ID: int64(r.Id), TaskType: r.TaskType, TypeVersion: int(r.TypeVersion), SubjectType: r.SubjectType, SubjectID: r.SubjectId,
		Status: TaskStatus(r.Status), Priority: TaskPriority(r.Priority), AssigneeID: r.AssigneeId, AssigneeGroup: r.AssigneeGroup,
		CurrentNode: r.CurrentNode, RunID: int64(r.RunId), Resolution: Resolution(r.Resolution), SLAStatus: SLAStatus(r.SlaStatus),
		EscalationLevel: int(r.EscalationLevel), DedupKey: r.DedupKey, Payload: json.RawMessage(r.Payload), Seq: r.Seq, Version: r.Version,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.SnoozeUntil.Valid {
		v := r.SnoozeUntil.Time
		t.SnoozeUntil = &v
	}
	if r.DueAt.Valid {
		v := r.DueAt.Time
		t.DueAt = &v
	}
	return t
}

// ParsePriority 把 "P0".."P3" 转为优先级，无法识别时返回 P2。
func ParsePriority(s string) TaskPriority {
	switch strings.ToUpper(s) {
	case "P0":
		return TaskPriorityP0
	case "P1":
		return TaskPriorityP1
	case "P3":
		return TaskPriorityP3
	default:
		return TaskPriorityP2
	}
}

// syncPayload 与引擎 sync_task job 的参数结构一致（engine.SyncPayload），任务中心不依赖引擎包。
type syncPayload struct {
	TaskID      int64      `json:"task_id"`
	Status      string     `json:"status"`
	Reassign    bool       `json:"reassign"`
	Assignee    string     `json:"assignee"`
	Group       string     `json:"group"`
	CurrentNode string     `json:"current_node"`
	Resolution  string     `json:"resolution"`
	SnoozeUntil *time.Time `json:"snooze_until"`
	Seq         int64      `json:"seq"`
}

// SyncAction 把引擎的 sync_task 动作接到任务中心，注册为 action.KindSyncTask。
type SyncAction struct{ Tasks Service }

// Kind 实现 action.Action。
func (SyncAction) Kind() action.Kind { return action.KindSyncTask }

// Invoke 实现 action.Action。
func (a SyncAction) Invoke(ctx context.Context, req action.Request) (action.Result, error) {
	var p syncPayload
	if err := json.Unmarshal(req.Input, &p); err != nil {
		return action.Result{}, fmt.Errorf("task: sync payload: %w", err)
	}
	status, ok := map[string]TaskStatus{
		"unassigned": TaskStatusUnassigned, "pending": TaskStatusPending, "snoozed": TaskStatusSnoozed, "solved": TaskStatusSolved,
	}[p.Status]
	if !ok {
		return action.Result{}, fmt.Errorf("task: unknown sync status %q", p.Status)
	}
	applied, err := a.Tasks.SyncStatus(ctx, SyncStatusCmd{
		TaskID: p.TaskID, Status: status, Reassign: p.Reassign, AssigneeID: p.Assignee, Group: p.Group, CurrentNode: p.CurrentNode,
		Resolution: Resolution(p.Resolution), SnoozeUntil: p.SnoozeUntil, Seq: p.Seq,
	})
	if err != nil {
		return action.Result{}, err
	}
	out, _ := json.Marshal(map[string]bool{"applied": applied})
	return action.Result{Output: out}, nil
}
