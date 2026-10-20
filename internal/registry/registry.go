// Package registry 是任务类型的配置中心：草稿、校验、直接发布与已发布版本。
//
// 一期没有变更集与审批：有 task_type.publish 权限的人直接发布（审批平台接入放二期）。
// 发布后版本不可变；在途任务按创建时的版本走完节点链，数据源扫描与字段刷新用最新版本。
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

// 错误。
var (
	ErrNotFound     = errors.New("registry: task type not found")
	ErrExists       = errors.New("registry: task type already exists")
	ErrConflict     = errors.New("registry: draft was changed by someone else")
	ErrNoDraft      = errors.New("registry: no draft")
	ErrUnpublished  = errors.New("registry: task type has no published version")
	ErrInvalidDraft = errors.New("registry: draft has issues")
)

// Refs 检查任务类型引用的外部对象是否存在（团队、数据源连接、业务关系）。
type Refs interface {
	TeamExists(ctx context.Context, key string) (bool, error)
	DatasourceExists(name string) bool
	RelationExists(name string) bool
}

// Summary 是任务类型列表中的一行。
type Summary struct {
	Key           string
	Namespace     string
	Name          string
	LatestVersion int
	HasDraft      bool
	UpdatedAt     time.Time
}

// Version 是一个已发布版本。
type Version struct {
	Key         string
	Version     int
	Body        json.RawMessage
	ContentHash string
	PublishedBy string
	PublishedAt time.Time
}

// Draft 是草稿。
type Draft struct {
	Key         string
	BaseVersion int // 0 = 尚未发布过
	Revision    int64
	Body        json.RawMessage
	UpdatedBy   string
}

// Registry 读写任务类型。
type Registry struct {
	conn     sqlx.SqlConn
	types    model.TaskTypeModel
	versions model.TaskTypeVersionModel
	sources  model.SourceStateModel
	Refs     Refs

	mu    sync.RWMutex
	cache map[string]*tasktype.TaskType // key@version → 解析后的定义（版本不可变，可以永久缓存）
}

// New 创建 Registry。
func New(conn sqlx.SqlConn, refs Refs) *Registry {
	return &Registry{conn: conn, types: model.NewTaskTypeModel(conn), versions: model.NewTaskTypeVersionModel(conn),
		sources: model.NewSourceStateModel(conn), Refs: refs, cache: map[string]*tasktype.TaskType{}}
}

// Load 返回某个已发布版本的定义。
func (r *Registry) Load(ctx context.Context, key string, version int) (*tasktype.TaskType, error) {
	ck := fmt.Sprintf("%s@%d", key, version)
	r.mu.RLock()
	t := r.cache[ck]
	r.mu.RUnlock()
	if t != nil {
		return t, nil
	}
	row, err := r.versions.FindOneByTypeKeyVersion(ctx, key, int64(version))
	if errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, ck)
	}
	if err != nil {
		return nil, err
	}
	if t, err = tasktype.Parse([]byte(row.Body)); err != nil {
		return nil, fmt.Errorf("registry: %s: %w", ck, err)
	}
	r.mu.Lock()
	r.cache[ck] = t
	r.mu.Unlock()
	return t, nil
}

// Latest 返回最新发布版本的定义与版本号。
func (r *Registry) Latest(ctx context.Context, key string) (*tasktype.TaskType, int, error) {
	row, err := r.types.FindOneByTypeKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, 0, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, 0, err
	}
	if row.LatestVersion == 0 {
		return nil, 0, fmt.Errorf("%w: %s", ErrUnpublished, key)
	}
	t, err := r.Load(ctx, key, int(row.LatestVersion))
	return t, int(row.LatestVersion), err
}

// List 返回任务类型列表。
func (r *Registry) List(ctx context.Context, keyword, namespace string) ([]Summary, error) {
	rows, err := r.types.List(ctx, keyword, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(rows))
	for _, row := range rows {
		out = append(out, Summary{Key: row.TypeKey, Namespace: row.Namespace, Name: row.Name, LatestVersion: int(row.LatestVersion),
			HasDraft: row.Draft.Valid, UpdatedAt: row.UpdatedAt})
	}
	return out, nil
}

// Get 返回任务类型的摘要。
func (r *Registry) Get(ctx context.Context, key string) (*Summary, error) {
	row, err := r.types.FindOneByTypeKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, err
	}
	return &Summary{Key: row.TypeKey, Namespace: row.Namespace, Name: row.Name, LatestVersion: int(row.LatestVersion),
		HasDraft: row.Draft.Valid, UpdatedAt: row.UpdatedAt}, nil
}

// Version 返回某个已发布版本；version 为 0 时返回最新版本。
func (r *Registry) Version(ctx context.Context, key string, version int) (*Version, error) {
	if version == 0 {
		s, err := r.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if s.LatestVersion == 0 {
			return nil, fmt.Errorf("%w: %s", ErrUnpublished, key)
		}
		version = s.LatestVersion
	}
	row, err := r.versions.FindOneByTypeKeyVersion(ctx, key, int64(version))
	if errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s@%d", ErrNotFound, key, version)
	}
	if err != nil {
		return nil, err
	}
	return toVersion(row), nil
}

// Versions 返回全部已发布版本，新版本在前。
func (r *Registry) Versions(ctx context.Context, key string) ([]Version, error) {
	rows, err := r.versions.ListByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(rows))
	for _, row := range rows {
		out = append(out, *toVersion(row))
	}
	return out, nil
}

func toVersion(row *model.TaskTypeVersion) *Version {
	return &Version{Key: row.TypeKey, Version: int(row.Version), Body: json.RawMessage(row.Body), ContentHash: row.ContentHash,
		PublishedBy: row.PublishedBy, PublishedAt: row.PublishedAt}
}

// Published 返回全部已发布任务类型的最新定义（数据源调度用）。
func (r *Registry) Published(ctx context.Context) (map[string]*tasktype.TaskType, error) {
	rows, err := r.types.List(ctx, "", "")
	if err != nil {
		return nil, err
	}
	out := map[string]*tasktype.TaskType{}
	for _, row := range rows {
		if row.LatestVersion == 0 {
			continue
		}
		t, err := r.Load(ctx, row.TypeKey, int(row.LatestVersion))
		if err != nil {
			return nil, err
		}
		out[row.TypeKey] = t
	}
	return out, nil
}

// Create 新建任务类型，生成一份最小草稿：SQL 来源、一个字段、一个节点。
func (r *Registry) Create(ctx context.Context, key, namespace, name, description, actor string) (*Draft, error) {
	body := tasktype.TaskType{
		Key: key, Namespace: namespace, Name: name, Description: description, Priority: "P2",
		Source: tasktype.Source{Kind: tasktype.SourceSQL, Schedule: "*/5 * * * *",
			Query: "select id as `key`, title from your_table where status = 'pending'"},
		Fields: []tasktype.Field{{Name: "title", Label: "标题"}},
		Card:   tasktype.Card{Title: name + "：{title}", Fields: []string{"title"}},
		Nodes: []tasktype.Node{{ID: "handle", Name: "处理", Actions: []string{tasktype.ActionClose, tasktype.ActionSnooze},
			Assignee: tasktype.Assignee{Kind: tasktype.AssigneeTeam, Mode: tasktype.ModeClaim}, Lark: true}},
	}
	raw, _ := json.Marshal(body)
	_, err := r.types.Insert(ctx, &model.TaskType{TypeKey: key, Namespace: namespace, Name: name, CreatedBy: actor,
		Draft: nullString(string(raw)), DraftUpdatedBy: actor})
	if err != nil {
		if isDuplicate(err) {
			return nil, fmt.Errorf("%w: %s", ErrExists, key)
		}
		return nil, err
	}
	return r.GetDraft(ctx, key, actor)
}

// GetDraft 返回草稿；没有草稿时基于最新发布版本创建一份。
func (r *Registry) GetDraft(ctx context.Context, key, actor string) (*Draft, error) {
	row, err := r.types.FindOneByTypeKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, err
	}
	if !row.Draft.Valid {
		v, err := r.Version(ctx, key, int(row.LatestVersion))
		if err != nil {
			return nil, err
		}
		if _, err := r.types.SaveDraft(ctx, key, row.DraftRevision, string(v.Body), actor); err != nil {
			return nil, err
		}
		if row, err = r.types.FindOneByTypeKey(ctx, key); err != nil {
			return nil, err
		}
	}
	return &Draft{Key: key, BaseVersion: int(row.LatestVersion), Revision: row.DraftRevision, Body: json.RawMessage(row.Draft.String),
		UpdatedBy: row.DraftUpdatedBy}, nil
}

// SaveDraft 保存草稿（乐观锁），返回新 revision 与校验问题。key 不能修改。
func (r *Registry) SaveDraft(ctx context.Context, key string, revision int64, body json.RawMessage, actor string) (int64, []tasktype.Issue, error) {
	var probe struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return 0, nil, fmt.Errorf("registry: body is not JSON: %w", err)
	}
	if probe.Key != key {
		return 0, nil, fmt.Errorf("registry: key cannot be changed (%q → %q)", key, probe.Key)
	}
	ok, err := r.types.SaveDraft(ctx, key, revision, string(body), actor)
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		return 0, nil, ErrConflict
	}
	_, issues := r.Validate(ctx, body)
	return revision + 1, issues, nil
}

// DiscardDraft 丢弃草稿。尚未发布过的任务类型连同记录一起删除。
func (r *Registry) DiscardDraft(ctx context.Context, key string) error {
	row, err := r.types.FindOneByTypeKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return err
	}
	if row.LatestVersion == 0 {
		return r.types.Delete(ctx, row.Id)
	}
	return r.types.ClearDraft(ctx, key)
}

// Validate 完整校验：结构、语义与外部引用（团队、数据源连接、业务关系）。
func (r *Registry) Validate(ctx context.Context, body json.RawMessage) (*tasktype.TaskType, []tasktype.Issue) {
	t, issues := tasktype.Validate(body)
	if t == nil || r.Refs == nil {
		return t, issues
	}
	if t.Source.Kind == tasktype.SourceSQL && !r.Refs.DatasourceExists(t.Source.Datasource) {
		issues = append(issues, tasktype.Issue{Path: "source.datasource", Message: fmt.Sprintf("数据源连接 %s 不存在", t.Source.Datasource)})
	}
	for i, n := range t.Nodes {
		team, path := n.Assignee.Team, fmt.Sprintf("nodes.%d.assignee.team", i)
		if n.Assignee.Kind == tasktype.AssigneeRelation {
			team, path = n.Assignee.FallbackTeam, fmt.Sprintf("nodes.%d.assignee.fallback_team", i)
			if !r.Refs.RelationExists(n.Assignee.Relation) {
				issues = append(issues, tasktype.Issue{Path: fmt.Sprintf("nodes.%d.assignee.relation", i), Message: fmt.Sprintf("业务关系 %s 不存在", n.Assignee.Relation)})
			}
		}
		if ok, err := r.Refs.TeamExists(ctx, team); err != nil || !ok {
			issues = append(issues, tasktype.Issue{Path: path, Message: fmt.Sprintf("团队 %s 不存在", team)})
		}
	}
	return t, issues
}

// Publish 校验草稿并发布为新版本，删除草稿。SQL 来源在发布后立即参与调度。
func (r *Registry) Publish(ctx context.Context, key, actor string, now time.Time) (*Version, []tasktype.Issue, error) {
	d, err := r.GetDraft(ctx, key, actor)
	if err != nil {
		return nil, nil, err
	}
	t, issues := r.Validate(ctx, d.Body)
	if len(issues) > 0 {
		return nil, issues, ErrInvalidDraft
	}
	v, err := r.publish(ctx, t, d.Body, actor, now)
	return v, nil, err
}

// PublishBody 直接发布一份任务类型 JSON（命令行与测试用，任务类型不存在时创建）。
func (r *Registry) PublishBody(ctx context.Context, body json.RawMessage, actor string, now time.Time) (*Version, []tasktype.Issue, error) {
	t, issues := r.Validate(ctx, body)
	if len(issues) > 0 {
		return nil, issues, ErrInvalidDraft
	}
	if _, err := r.types.FindOneByTypeKey(ctx, t.Key); errors.Is(err, model.ErrNotFound) {
		if _, err := r.types.Insert(ctx, &model.TaskType{TypeKey: t.Key, Namespace: t.Namespace, Name: t.Name, CreatedBy: actor}); err != nil && !isDuplicate(err) {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}
	v, err := r.publish(ctx, t, body, actor, now)
	return v, nil, err
}

func (r *Registry) publish(ctx context.Context, t *tasktype.TaskType, body json.RawMessage, actor string, now time.Time) (*Version, error) {
	canonical, err := json.Marshal(t) // 去掉未知格式差异，内容哈希只看语义
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	var out *Version
	err = r.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		types, versions := r.types.WithSession(session), r.versions.WithSession(session)
		row, err := types.FindOneForUpdate(ctx, t.Key)
		if err != nil {
			return err
		}
		next := row.LatestVersion + 1
		v := &model.TaskTypeVersion{TypeKey: t.Key, Version: next, Body: string(canonical), ContentHash: hex.EncodeToString(sum[:]),
			PublishedBy: actor, PublishedAt: now}
		if _, err := versions.Insert(ctx, v); err != nil {
			return err
		}
		if err := types.SetPublished(ctx, t.Key, next, t.Name, t.Namespace); err != nil {
			return err
		}
		out = toVersion(v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if t.Source.Kind == tasktype.SourceSQL {
		if err := r.sources.Ensure(ctx, t.Key, now); err != nil {
			return nil, err
		}
	}
	return out, nil
}
