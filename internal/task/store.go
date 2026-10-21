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

	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/notify"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// ErrNotFound 表示任务不存在。
var ErrNotFound = errors.New("task: not found")

// Defs 加载任务固定的任务类型版本。
type Defs interface {
	Load(ctx context.Context, key string, version int) (*tasktype.TaskType, error)
}

// Store 是任务中心：一次事件 = 一个短事务（锁任务行 → Apply → 写任务、历史、经手人、outbox），提交后投递通知。
type Store struct {
	conn    sqlx.SqlConn
	tasks   model.TaskModel
	events  model.TaskEventModel
	parts   model.TaskParticipantModel
	outbox  model.OutboxModel
	Defs    Defs
	Notify  *notify.Dispatcher
	Now     func() time.Time
	TaskURL func(id int64) string // 工作台任务详情的地址，写进 Lark 卡片的按钮
}

// NewStore 创建 Store。
func NewStore(conn sqlx.SqlConn, defs Defs, d *notify.Dispatcher, now func() time.Time) *Store {
	return &Store{conn: conn, tasks: model.NewTaskModel(conn), events: model.NewTaskEventModel(conn),
		parts: model.NewTaskParticipantModel(conn), outbox: model.NewOutboxModel(conn), Defs: defs, Notify: d, Now: now,
		TaskURL: func(id int64) string { return fmt.Sprintf("/tasks/%d", id) }}
}

// Get 返回任务。
func (s *Store) Get(ctx context.Context, id int64) (*Task, error) {
	row, err := s.tasks.FindOne(ctx, uint64(id))
	if errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	t := fromRow(row)
	return &t, nil
}

// CreateCmd 创建任务。
type CreateCmd struct {
	TaskType string
	Version  int
	Key      string
	Fields   json.RawMessage
	SourceAt int64
	Target   assign.Target // 第一个节点的处理人，调用方在事务外求好
}

// Create 创建任务。同一任务类型同一 key 已有跟踪中的任务时返回 created=false。
func (s *Store) Create(ctx context.Context, cmd CreateCmd) (*Task, bool, error) {
	def, err := s.Defs.Load(ctx, cmd.TaskType, cmd.Version)
	if err != nil {
		return nil, false, err
	}
	res, err := Apply(def, &Task{TaskType: cmd.TaskType, TypeVersion: cmd.Version, BizKey: cmd.Key, Fields: cmd.Fields, SourceAt: cmd.SourceAt},
		Event{Kind: EvCreate, Now: s.Now(), Target: cmd.Target})
	if err != nil {
		return nil, false, err
	}
	t := res.Task
	var kick []uint64
	err = s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		row := toRow(t)
		r, err := s.tasks.WithSession(session).Insert(ctx, row)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		t.ID = id
		kick, err = s.record(ctx, session, def, nil, t, EvCreate, "", res)
		return err
	})
	if err != nil {
		if isDuplicate(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	s.Notify.Kick(kick)
	return &t, true, nil
}

// Do 对任务应用一次事件。成员关系等身份信息在锁内按任务当前的团队求值。
func (s *Store) Do(ctx context.Context, id int64, ev Event) (*Task, error) {
	if ev.Now.IsZero() {
		ev.Now = s.Now()
	}
	var out Task
	var kick []uint64
	err := s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		row, err := s.tasks.WithSession(session).FindOneForUpdate(ctx, uint64(id))
		if errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("%w: %d", ErrNotFound, id)
		}
		if err != nil {
			return err
		}
		cur := fromRow(row)
		def, err := s.Defs.Load(ctx, cur.TaskType, cur.TypeVersion)
		if err != nil {
			return err
		}
		res, err := Apply(def, &cur, ev)
		if err != nil {
			return err
		}
		next := toRow(res.Task)
		next.Id = row.Id
		if err := s.tasks.WithSession(session).Update(ctx, next); err != nil {
			return err
		}
		out = res.Task
		kick, err = s.record(ctx, session, def, &cur, res.Task, ev.Kind, ev.Actor, res)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.Notify.Kick(kick)
	return &out, nil
}

// record 在事务内写历史、经手人与 outbox，返回需要立即投递的 outbox id。
func (s *Store) record(ctx context.Context, session sqlx.Session, def *tasktype.TaskType, from *Task, to Task, kind EventKind, actor string, res Result) ([]uint64, error) {
	fromStatus := to.Status
	if from != nil {
		fromStatus = from.Status
	}
	if actor == "" {
		actor = "system"
	}
	detail, _ := json.Marshal(res.Detail)
	ev := &model.TaskEvent{TaskId: uint64(to.ID), Kind: string(kind), Actor: actor, NodeId: to.NodeID,
		FromStatus: int64(fromStatus), ToStatus: int64(to.Status), Detail: sql.NullString{String: string(detail), Valid: len(res.Detail) > 0}}
	if kind == EvAct {
		ev.Kind = fmt.Sprint(res.Detail["action"])
	}
	if from != nil && from.NodeID != to.NodeID {
		ev.NodeId = from.NodeID // escalate：记在离开的节点上
	}
	if _, err := s.events.WithSession(session).Insert(ctx, ev); err != nil {
		return nil, err
	}
	parts := s.parts.WithSession(session)
	for _, u := range []string{res.Participant, to.AssigneeID} {
		if u != "" {
			if err := parts.Add(ctx, uint64(to.ID), u); err != nil {
				return nil, err
			}
		}
	}
	if len(res.Notify) == 0 {
		return nil, nil
	}
	card := def.RenderCard(to.NodeID, to.BizKey, tasktype.DecodeValues(to.Fields), s.TaskURL(to.ID))
	payload, _ := json.Marshal(card)
	ob := s.outbox.WithSession(session)
	var ids []uint64
	for _, n := range res.Notify {
		key := fmt.Sprintf("%d:%d:%s:%s", to.ID, to.Version, notify.KindUser, n.User)
		if err := ob.Add(ctx, &model.Outbox{TaskId: uint64(to.ID), Kind: notify.KindUser, Target: n.User, Payload: string(payload), IdemKey: key, NextAt: s.Now()}); err != nil {
			return nil, err
		}
		o, err := ob.FindOneByIdemKey(ctx, key)
		if err != nil {
			return nil, err
		}
		ids = append(ids, o.Id)
	}
	return ids, nil
}

// ListTracked 返回某个任务类型全部跟踪中的任务。
func (s *Store) ListTracked(ctx context.Context, taskType string) ([]Task, error) {
	rows, err := s.tasks.ListTracked(ctx, taskType)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, nil
}

// RefreshFields 刷新业务字段（数据源再次出现时），不改变状态，不重发通知。
func (s *Store) RefreshFields(ctx context.Context, id int64, fields json.RawMessage, sourceAt int64) error {
	return s.tasks.UpdateFields(ctx, uint64(id), string(fields), sourceAt)
}

// WakeDue 把推迟到期的任务回迁待处理，返回处理条数。
func (s *Store) WakeDue(ctx context.Context, limit int) (int, error) {
	ids, err := s.tasks.DueSnoozed(ctx, s.Now(), limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if _, err := s.Do(ctx, int64(id), Event{Kind: EvWake}); err != nil && !errors.Is(err, ErrState) {
			return n, err
		}
		n++
	}
	return n, nil
}

func fromRow(r *model.Task) Task {
	t := Task{
		ID: int64(r.Id), TaskType: r.TaskType, TypeVersion: int(r.TypeVersion), BizKey: r.BizKey, Tracked: r.TrackKey.Valid,
		Fields: json.RawMessage(r.Fields), NodeID: r.NodeId, Status: TaskStatus(r.Status), Priority: TaskPriority(r.Priority),
		AssigneeID: r.AssigneeId, Resolution: Resolution(r.Resolution),
		SourceAt: r.SourceAt, Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.SnoozeUntil.Valid {
		v := r.SnoozeUntil.Time
		t.SnoozeUntil = &v
	}
	if r.ClosedAt.Valid {
		v := r.ClosedAt.Time
		t.ClosedAt = &v
	}
	return t
}

func toRow(t Task) *model.Task {
	r := &model.Task{
		Id: uint64(t.ID), TaskType: t.TaskType, TypeVersion: int64(t.TypeVersion), BizKey: t.BizKey, Fields: string(t.Fields),
		NodeId: t.NodeID, Status: int64(t.Status), Priority: int64(t.Priority), AssigneeId: t.AssigneeID,
		Resolution: string(t.Resolution), SourceAt: t.SourceAt, Version: t.Version,
	}
	if len(t.Fields) == 0 {
		r.Fields = "{}"
	}
	if t.Tracked {
		r.TrackKey = sql.NullString{String: t.BizKey, Valid: true}
	}
	if t.SnoozeUntil != nil {
		r.SnoozeUntil = sql.NullTime{Time: *t.SnoozeUntil, Valid: true}
	}
	if t.ClosedAt != nil {
		r.ClosedAt = sql.NullTime{Time: *t.ClosedAt, Valid: true}
	}
	return r
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}
