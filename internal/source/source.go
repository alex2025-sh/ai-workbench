// Package source 把数据源归一成「某个 key 当前在 / 不在待办集合里」：
//
//   - SQL 来源：平台按 cron 执行查询，结果集就是当前待办。新出现的 key 创建任务，消失的 key 自动关闭任务，
//     仍在的刷新业务字段（不重发通知）。
//   - API / Kafka 来源：业务方推送 upsert（在）或 close（不在），规则相同；按 occurred_at 丢弃过期消息。
//
// 处理人手工完结的任务仍「跟踪中」：key 还在结果里时不会重建，等它消失一次、再出现才是新任务。
package source

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/task"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// 执行 SQL 来源的限制。
const (
	QueryTimeout = 10 * time.Second
	MaxRows      = 5000
	leaseFor     = 5 * time.Minute
)

// Registry 提供已发布的任务类型。
type Registry interface {
	Latest(ctx context.Context, key string) (*tasktype.TaskType, int, error)
	Published(ctx context.Context) (map[string]*tasktype.TaskType, error)
}

// Datasources 是平台注册的只读数据源连接：名字 → *sql.DB（只读从库账号）。
type Datasources map[string]*sql.DB

// Exists 报告连接是否已注册。
func (d Datasources) Exists(name string) bool { _, ok := d[name]; return ok }

// Names 返回已注册的连接名，按名字排序。
func (d Datasources) Names() []string {
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Row 是一条记录：key 与其余列。
type Row struct {
	Key    string
	Values map[string]any
}

// Result 是一次查询的结果。
type Result struct {
	Columns   []string // 不含 key
	Rows      []Row
	Truncated bool // 超过 MaxRows：只创建、不关闭
}

// Query 在只读事务中执行 SQL 来源的查询，最多返回 limit 行（多取一行判断是否截断）。
func (d Datasources) Query(ctx context.Context, name, query string, limit int) (*Result, error) {
	db, ok := d[name]
	if !ok {
		return nil, fmt.Errorf("source: datasource %q is not registered", name)
	}
	if err := tasktype.CheckQuery(query); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	q := strings.TrimSuffix(strings.TrimSpace(query), ";")
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("select * from (%s) as wb_source limit %d", q, limit+1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	keyIdx := -1
	res := &Result{}
	for i, c := range cols {
		if c == tasktype.KeyPlaceholder {
			keyIdx = i
		} else {
			res.Columns = append(res.Columns, c)
		}
	}
	if keyIdx < 0 {
		return nil, fmt.Errorf("source: query must return a column named key")
	}
	for rows.Next() {
		if len(res.Rows) == limit {
			res.Truncated = true
			break
		}
		raw := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		r := Row{Values: map[string]any{}}
		for i, c := range cols {
			switch {
			case i == keyIdx:
				r.Key = string(raw[i])
			case raw[i] == nil:
				r.Values[c] = nil
			default:
				r.Values[c] = string(raw[i])
			}
		}
		if r.Key == "" {
			continue // 没有 key 的行无法去重，丢弃
		}
		res.Rows = append(res.Rows, r)
	}
	return res, rows.Err()
}

// Runner 执行数据源。
type Runner struct {
	Registry    Registry
	Datasources Datasources
	Tasks       *task.Store
	Assign      *assign.Service
	States      model.SourceStateModel
	Now         func() time.Time
}

// Stats 是一次同步的结果。
type Stats struct {
	TaskType string
	Rows     int
	Created  int
	Closed   int
	Err      error
}

// RunDue 执行到期的 SQL 来源。多实例下同一个来源只有抢到租约的实例执行。
func (r *Runner) RunDue(ctx context.Context) []Stats {
	types, err := r.Registry.Published(ctx)
	if err != nil {
		return []Stats{{Err: err}}
	}
	keys := make([]string, 0, len(types))
	for k, t := range types {
		if t.Source.Kind == tasktype.SourceSQL {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []Stats
	for _, k := range keys {
		t := types[k]
		now := r.Now()
		next, err := nextRun(t.Source, now)
		if err != nil {
			out = append(out, Stats{TaskType: k, Err: err})
			continue
		}
		if err := r.States.Ensure(ctx, k, now); err != nil {
			out = append(out, Stats{TaskType: k, Err: err})
			continue
		}
		ok, err := r.States.Lease(ctx, k, now, now.Add(leaseFor), next)
		if err != nil || !ok {
			if err != nil {
				out = append(out, Stats{TaskType: k, Err: err})
			}
			continue
		}
		st := r.RunSQL(ctx, k)
		errText := ""
		if st.Err != nil {
			errText = st.Err.Error()
		}
		if err := r.States.Finish(ctx, k, r.Now(), st.Rows, st.Created, st.Closed, errText); err != nil && st.Err == nil {
			st.Err = err
		}
		out = append(out, st)
	}
	return out
}

func nextRun(s tasktype.Source, now time.Time) (time.Time, error) {
	sched, err := tasktype.CronParser.Parse(s.Schedule)
	if err != nil {
		return time.Time{}, err
	}
	loc := tasktype.DisplayLoc
	if s.Timezone != "" {
		if l, err := time.LoadLocation(s.Timezone); err == nil {
			loc = l
		}
	}
	return sched.Next(now.In(loc)), nil
}

// RunSQL 立即执行一个任务类型的 SQL 来源并同步（不看调度时间）。
func (r *Runner) RunSQL(ctx context.Context, key string) Stats {
	st := Stats{TaskType: key}
	def, version, err := r.Registry.Latest(ctx, key)
	if err != nil {
		st.Err = err
		return st
	}
	res, err := r.Datasources.Query(ctx, def.Source.Datasource, def.Source.Query, MaxRows)
	if err != nil {
		st.Err = fmt.Errorf("query: %w", err)
		return st
	}
	st.Rows = len(res.Rows)
	present := make(map[string]Row, len(res.Rows))
	for _, row := range res.Rows {
		present[row.Key] = row
	}
	tracked, err := r.Tasks.ListTracked(ctx, key)
	if err != nil {
		st.Err = err
		return st
	}
	seen := make(map[string]task.Task, len(tracked))
	for _, t := range tracked {
		seen[t.BizKey] = t
	}
	var errs []error
	// 新出现的 key：按结果顺序创建。
	for _, row := range res.Rows {
		t, ok := seen[row.Key]
		if ok {
			if err := r.refresh(ctx, t, row.Values, 0); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		created, err := r.create(ctx, def, version, row.Key, row.Values, 0)
		if err != nil {
			errs = append(errs, fmt.Errorf("create %s: %w", row.Key, err))
		} else if created {
			st.Created++
		}
	}
	// 消失的 key：结果被截断时无法判断，跳过关闭。
	if res.Truncated {
		errs = append(errs, fmt.Errorf("query returned more than %d rows; auto-close skipped", MaxRows))
	} else {
		for _, t := range tracked {
			if _, ok := present[t.BizKey]; ok {
				continue
			}
			wasOpen := !t.Status.IsTerminal()
			if _, err := r.Tasks.Do(ctx, t.ID, task.Event{Kind: task.EvGone}); err != nil {
				errs = append(errs, fmt.Errorf("close %s: %w", t.BizKey, err))
			} else if wasOpen {
				st.Closed++
			}
		}
	}
	st.Err = errors.Join(errs...)
	if st.Err != nil {
		logx.WithContext(ctx).Errorf("source %s: %v", key, st.Err)
	}
	return st
}

func (r *Runner) create(ctx context.Context, def *tasktype.TaskType, version int, key string, vals map[string]any, sourceAt int64) (bool, error) {
	to, err := r.Assign.Resolve(ctx, def.Nodes[0], assign.WithKey(vals, key))
	if err != nil {
		return false, err
	}
	fields, err := json.Marshal(vals)
	if err != nil {
		return false, err
	}
	_, created, err := r.Tasks.Create(ctx, task.CreateCmd{TaskType: def.Key, Version: version, Key: key, Fields: fields, SourceAt: sourceAt, Target: to})
	return created, err
}

func (r *Runner) refresh(ctx context.Context, t task.Task, vals map[string]any, sourceAt int64) error {
	fields, err := json.Marshal(vals)
	if err != nil {
		return err
	}
	if sameJSON(t.Fields, fields) && sourceAt <= t.SourceAt {
		return nil
	}
	return r.Tasks.RefreshFields(ctx, t.ID, fields, sourceAt)
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}

// 推送的操作。
const (
	OpUpsert = "upsert"
	OpClose  = "close"
)

// Envelope 是 API / Kafka 推送的消息。
type Envelope struct {
	TaskType   string          `json:"task_type"`
	Key        string          `json:"key"`
	Op         string          `json:"op"`
	OccurredAt int64           `json:"occurred_at"` // 毫秒
	Fields     json.RawMessage `json:"fields,omitempty"`
}

// ErrBadEnvelope 表示推送的消息不合法（API 直接返回错误，Kafka 记日志并计数）。
var ErrBadEnvelope = errors.New("source: bad envelope")

// Push 处理一条推送。返回是否产生了变化（创建或关闭）。
func (r *Runner) Push(ctx context.Context, env Envelope) (bool, error) {
	if env.Key == "" || env.OccurredAt <= 0 {
		return false, fmt.Errorf("%w: key and occurred_at are required", ErrBadEnvelope)
	}
	def, version, err := r.Registry.Latest(ctx, env.TaskType)
	if err != nil {
		return false, err
	}
	if def.Source.Kind != tasktype.SourceAPI && def.Source.Kind != tasktype.SourceKafka {
		return false, fmt.Errorf("%w: task type %s does not accept pushes", ErrBadEnvelope, env.TaskType)
	}
	var cur *task.Task
	tracked, err := r.Tasks.ListTracked(ctx, env.TaskType) // 推送量大时改为按 key 查询
	if err != nil {
		return false, err
	}
	for i := range tracked {
		if tracked[i].BizKey == env.Key {
			cur = &tracked[i]
		}
	}
	if cur != nil && env.OccurredAt < cur.SourceAt {
		return false, nil // 比已应用的消息旧：乱序或重复投递
	}
	switch env.Op {
	case OpUpsert:
		vals, err := decodeFields(def, env.Fields)
		if err != nil {
			return false, err
		}
		if cur != nil {
			return false, r.refresh(ctx, *cur, vals, env.OccurredAt)
		}
		return r.create(ctx, def, version, env.Key, vals, env.OccurredAt)
	case OpClose:
		if cur == nil {
			return false, nil
		}
		wasOpen := !cur.Status.IsTerminal()
		_, err := r.Tasks.Do(ctx, cur.ID, task.Event{Kind: task.EvGone})
		return wasOpen && err == nil, err
	default:
		return false, fmt.Errorf("%w: op must be upsert or close", ErrBadEnvelope)
	}
}

// decodeFields 按字段定义校验推送的 fields：不认识的字段报错，金额与数字保持字符串精度。
func decodeFields(def *tasktype.TaskType, raw json.RawMessage) (map[string]any, error) {
	vals := map[string]any{}
	if len(raw) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&vals); err != nil {
			return nil, fmt.Errorf("%w: fields: %v", ErrBadEnvelope, err)
		}
	}
	for k, v := range vals {
		if _, ok := def.Field(k); !ok {
			return nil, fmt.Errorf("%w: unknown field %q", ErrBadEnvelope, k)
		}
		switch v.(type) {
		case string, json.Number, bool, nil:
		default:
			return nil, fmt.Errorf("%w: field %q must be a scalar", ErrBadEnvelope, k)
		}
	}
	return vals, nil
}
