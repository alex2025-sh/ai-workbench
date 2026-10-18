package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/cursor"
)

// RunQuery 是运行监控的列表条件，空值表示不限。试运行实例不出现在列表中。
type RunQuery struct {
	IDs           []int64 // 按任务、biz_key 查询时由调用方先解析出 run
	Status        RunStatus
	PlanKey       string
	CreatedAfter  time.Time
	CreatedBefore time.Time
	Cursor        string // 上一页返回的 next；空 = 第一页
	PageSize      int
}

// RunListItem 是列表中的一个 run。
type RunListItem struct {
	Run       Run
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Event 是 run 的一条事件（节点状态变化、人工操作、隔离等）。
type Event struct {
	ID        int64
	NodeID    string
	Iteration int
	Type      string
	Payload   json.RawMessage
	Actor     string
	CreatedAt time.Time
}

// ListRuns 按创建时间倒序分页查询 run，返回下一页的游标（空 = 没有更多）。
func (e *Engine) ListRuns(ctx context.Context, q RunQuery) ([]RunListItem, string, error) {
	f := model.RunFilter{Status: string(q.Status), PlanID: q.PlanKey, CreatedAfter: q.CreatedAfter, CreatedBefore: q.CreatedBefore, Limit: cursor.Size(q.PageSize) + 1}
	for _, id := range q.IDs {
		f.IDs = append(f.IDs, uint64(id))
	}
	if q.Cursor != "" {
		c, err := cursor.Decode(q.Cursor, 2)
		if err != nil {
			return nil, "", err
		}
		f.Before = &model.RunCursor{CreatedAt: time.UnixMicro(c[0]).UTC(), ID: uint64(c[1])}
	}
	rows, err := e.store.runs.List(ctx, f)
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(rows) == f.Limit {
		rows = rows[:len(rows)-1]
		last := rows[len(rows)-1]
		next = cursor.Encode(last.CreatedAt.UnixMicro(), int64(last.Id))
	}
	out := make([]RunListItem, 0, len(rows))
	for _, r := range rows {
		run, err := runFromRow(r)
		if err != nil {
			return nil, "", err
		}
		out = append(out, RunListItem{Run: run, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
	}
	return out, next, nil
}

// RunEvents 按写入顺序分页返回 run 的事件。
func (e *Engine) RunEvents(ctx context.Context, runID int64, after string, size int) ([]Event, string, error) {
	limit := cursor.Size(size) + 1
	var from *model.EventCursor
	if after != "" {
		c, err := cursor.Decode(after, 2)
		if err != nil {
			return nil, "", err
		}
		from = &model.EventCursor{CreatedAt: time.UnixMicro(c[0]).UTC(), ID: uint64(c[1])}
	}
	rows, err := model.NewWfEventModel(e.store.conn).ListByRun(ctx, uint64(runID), from, limit)
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(rows) == limit {
		rows = rows[:len(rows)-1]
		last := rows[len(rows)-1]
		next = cursor.Encode(last.CreatedAt.UnixMicro(), int64(last.Id))
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		ev := Event{ID: int64(r.Id), NodeID: r.NodeId, Iteration: int(r.Iteration), Type: r.Type, Actor: r.Actor, CreatedAt: r.CreatedAt}
		if r.Payload.Valid {
			ev.Payload = json.RawMessage(r.Payload.String)
		}
		out = append(out, ev)
	}
	return out, next, nil
}

// RunByBizKey 按业务幂等键查找 run；不存在时返回 nil。
func (e *Engine) RunByBizKey(ctx context.Context, bizKey string) (*Run, error) {
	return e.store.findRunByBizKey(ctx, bizKey)
}
