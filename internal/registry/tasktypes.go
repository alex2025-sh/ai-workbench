package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

// 任务类型的草稿与开发环境的直接发布。变更集与审批尚未实现：发布复用 PublishDirect 写 wf_plan，
// 同时把配置对象登记在 cfg_component（latest_version），草稿存在 cfg_draft（每个任务类型最多一份）。
//
// 用 wbctl 直接发布、没有经过这里的任务类型在 cfg_component 中没有记录；第一次打开草稿时补登记。

// 草稿操作的错误。
var (
	ErrExists       = errors.New("registry: task type already exists")
	ErrNoTaskType   = errors.New("registry: task type not found")
	ErrNoDraft      = errors.New("registry: no draft")
	ErrConflict     = errors.New("registry: draft revision conflict")
	ErrKeyChanged   = errors.New("registry: key in the body does not match the task type")
	ErrHasErrors    = errors.New("registry: draft has errors")
	ErrDraftInvalid = errors.New("registry: draft is not valid JSON")
)

// TaskTypeDraft 是任务类型的草稿。
type TaskTypeDraft struct {
	Key         string
	BaseVersion int // 0 = 尚未发布过
	Revision    int64
	Body        json.RawMessage
	ContentHash string // 去掉 ui 后的内容哈希：只移动画布坐标时不变
	UpdatedBy   string
	UpdatedAt   time.Time
}

// TaskTypeEntry 是任务类型列表中的一项。
type TaskTypeEntry struct {
	Key, Name, Namespace string
	LatestVersion        int
	HasDraft             bool
	UpdatedAt            time.Time
}

// TaskTypes 管理任务类型的草稿与开发环境发布。
type TaskTypes struct {
	conn       sqlx.SqlConn
	components model.CfgComponentModel
	drafts     model.CfgDraftModel
	plans      model.WfPlanModel
	clock      func() time.Time
}

// NewTaskTypes 创建 TaskTypes。
func NewTaskTypes(conn sqlx.SqlConn) *TaskTypes {
	return &TaskTypes{conn: conn, components: model.NewCfgComponentModel(conn), drafts: model.NewCfgDraftModel(conn),
		plans: model.NewWfPlanModel(conn), clock: time.Now}
}

// Skeleton 是新建任务类型的初始内容：一个字段、一个默认卡片模板、处理人为创建者，流程只有开始与结束。
func Skeleton(key, name, namespace, description, creator string) json.RawMessage {
	body := map[string]any{
		"key": key, "namespace": namespace, "name": name, "description": description,
		"schema": map[string]any{"type": "object", "properties": map[string]any{
			"title": map[string]any{"type": "string", "title": "标题"},
		}},
		"views": map[string]any{"default_card": map[string]any{
			"title": "{{ payload.title }}", "fields": []any{map[string]any{"label": "标题", "value": "{{ payload.title }}"}},
		}},
		"view":       "default_card",
		"assignment": map[string]any{"resolvers": []any{map[string]any{"type": "user", "users": []string{creator}}}},
		"workflow": map[string]any{
			"nodes": []any{map[string]any{"id": "start", "type": "trigger"}, map[string]any{"id": "end", "type": "end"}},
			"edges": []any{map[string]any{"id": "e1", "from": "start", "port": "next", "to": "end"}},
		},
	}
	raw, _ := json.Marshal(body)
	return raw
}

// Create 新建任务类型：登记配置对象并创建初始草稿。key 已被使用（包括 wbctl 发布过的）时返回 ErrExists。
func (r *TaskTypes) Create(ctx context.Context, key, name, namespace, description, actor string) (*TaskTypeDraft, error) {
	if max, err := r.plans.MaxVersion(ctx, key); err != nil {
		return nil, err
	} else if max > 0 {
		return nil, ErrExists
	}
	body := Skeleton(key, name, namespace, description, actor)
	err := r.conn.TransactCtx(ctx, func(ctx context.Context, s sqlx.Session) error {
		res, err := r.components.WithSession(s).Insert(ctx, &model.CfgComponent{
			Namespace: namespace, Kind: string(KindTaskType), CompKey: key, Name: name, Description: description,
			Status: "active", CreatedBy: actor,
		})
		if isDuplicate(err) {
			return ErrExists
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		_, err = r.drafts.WithSession(s).Insert(ctx, &model.CfgDraft{ComponentId: uint64(id), Body: string(body), UpdatedBy: actor})
		return err
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, key, actor)
}

// Get 返回草稿；没有草稿时基于最新发布版本创建。任务类型不存在时返回 ErrNoTaskType。
func (r *TaskTypes) Get(ctx context.Context, key, actor string) (*TaskTypeDraft, error) {
	comp, err := r.component(ctx, key)
	if err != nil {
		return nil, err
	}
	d, err := r.drafts.FindOneByComponentId(ctx, comp.Id)
	if errors.Is(err, model.ErrNotFound) {
		if d, err = r.createFromLatest(ctx, comp, actor); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return toDraft(key, d), nil
}

// createFromLatest 基于最新发布版本创建草稿。
func (r *TaskTypes) createFromLatest(ctx context.Context, comp *model.CfgComponent, actor string) (*model.CfgDraft, error) {
	if comp.LatestVersion == 0 {
		return nil, ErrNoTaskType
	}
	plan, err := r.plans.FindOneByPlanIdVersion(ctx, comp.CompKey, comp.LatestVersion)
	if err != nil {
		return nil, err
	}
	_, err = r.drafts.Insert(ctx, &model.CfgDraft{ComponentId: comp.Id, BaseVersion: comp.LatestVersion, Body: plan.Dsl, UpdatedBy: actor})
	if err != nil && !isDuplicate(err) { // 并发打开时另一方已经创建
		return nil, err
	}
	return r.drafts.FindOneByComponentId(ctx, comp.Id)
}

// Find 返回草稿，没有草稿时返回 ErrNoDraft，不自动创建。
func (r *TaskTypes) Find(ctx context.Context, key string) (*TaskTypeDraft, error) {
	d, err := r.draftRow(ctx, key)
	if err != nil {
		return nil, err
	}
	return toDraft(key, d), nil
}

func (r *TaskTypes) draftRow(ctx context.Context, key string) (*model.CfgDraft, error) {
	comp, err := r.component(ctx, key)
	if err != nil {
		return nil, err
	}
	d, err := r.drafts.FindOneByComponentId(ctx, comp.Id)
	if errors.Is(err, model.ErrNotFound) {
		return nil, ErrNoDraft
	}
	return d, err
}

// Save 在 revision 一致时保存草稿，返回新的草稿与快速校验结果（有问题也照样保存）。
// 内容必须是 JSON 对象，且 key 与任务类型一致（key 不可修改）。
func (r *TaskTypes) Save(ctx context.Context, key string, revision int64, body json.RawMessage, actor string) (*TaskTypeDraft, []compiler.Issue, error) {
	var head struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return nil, nil, ErrDraftInvalid
	}
	if head.Key != key {
		return nil, nil, ErrKeyChanged
	}
	row, err := r.draftRow(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	ok, err := r.drafts.SaveIfRevision(ctx, row.Id, revision, string(body), actor)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, ErrConflict
	}
	saved, err := r.Find(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	return saved, spec.Check(body).Issues, nil
}

// Validate 对草稿做完整校验。
func (r *TaskTypes) Validate(ctx context.Context, key string) ([]compiler.Issue, error) {
	d, err := r.Find(ctx, key)
	if err != nil {
		return nil, err
	}
	return spec.Check(d.Body).Issues, nil
}

// Discard 在 revision 一致时丢弃草稿。
func (r *TaskTypes) Discard(ctx context.Context, key string, revision int64) error {
	d, err := r.draftRow(ctx, key)
	if err != nil {
		return err
	}
	ok, err := r.drafts.DeleteIfRevision(ctx, d.Id, revision)
	if err != nil {
		return err
	}
	if !ok {
		return ErrConflict
	}
	return nil
}

// PublishDraft 把草稿直接发布为新版本（仅开发环境；正式发布走变更集与审批）：
// 有 error 级问题时返回 ErrHasErrors 与问题列表；发布成功后删除草稿。
func (r *TaskTypes) PublishDraft(ctx context.Context, key string, revision int64, actor string) (int, []compiler.Issue, error) {
	d, err := r.Find(ctx, key)
	if err != nil {
		return 0, nil, err
	}
	if d.Revision != revision {
		return 0, nil, ErrConflict
	}
	res, err := PublishDirect(ctx, r.conn, d.Body, actor, r.clock())
	if err != nil {
		return 0, nil, err
	}
	if res.Version == 0 {
		return 0, res.Issues, ErrHasErrors
	}
	comp, err := r.component(ctx, key)
	if err != nil {
		return 0, nil, err
	}
	comp.LatestVersion = int64(res.Version)
	if t, err := spec.Parse(d.Body); err == nil {
		comp.Name, comp.Namespace, comp.Description = t.Name, t.Namespace, t.Description
	}
	if err := r.components.Update(ctx, comp); err != nil {
		return 0, nil, err
	}
	row, err := r.drafts.FindOneByComponentId(ctx, comp.Id)
	if err == nil {
		_, err = r.drafts.DeleteIfRevision(ctx, row.Id, revision)
	}
	return res.Version, res.Issues, err
}

// List 返回全部任务类型：已发布的（wf_plan）与新建尚未发布的（cfg_component），按 key 排序。
func (r *TaskTypes) List(ctx context.Context) ([]TaskTypeEntry, error) {
	byKey := map[string]*TaskTypeEntry{}
	latest, err := r.plans.Latest(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range latest {
		e := &TaskTypeEntry{Key: p.PlanId, LatestVersion: int(p.Version), UpdatedAt: p.PublishedAt}
		if t, err := spec.Parse([]byte(p.Dsl)); err == nil {
			e.Name, e.Namespace = t.Name, t.Namespace
		}
		byKey[p.PlanId] = e
	}
	comps, err := r.components.ListByKind(ctx, string(KindTaskType))
	if err != nil {
		return nil, err
	}
	withDraft, err := r.drafts.ComponentIDs(ctx)
	if err != nil {
		return nil, err
	}
	drafts := map[uint64]bool{}
	for _, id := range withDraft {
		drafts[id] = true
	}
	for _, c := range comps {
		e := byKey[c.CompKey]
		if e == nil {
			e = &TaskTypeEntry{Key: c.CompKey, Name: c.Name, Namespace: c.Namespace, UpdatedAt: c.UpdatedAt}
			byKey[c.CompKey] = e
		}
		e.HasDraft = drafts[c.Id]
	}
	out := make([]TaskTypeEntry, 0, len(byKey))
	for _, e := range byKey {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// component 返回任务类型的配置对象；只用 wbctl 发布过的任务类型在这里补登记。
func (r *TaskTypes) component(ctx context.Context, key string) (*model.CfgComponent, error) {
	c, err := r.components.FindOneByKindCompKey(ctx, string(KindTaskType), key)
	if err == nil || !errors.Is(err, model.ErrNotFound) {
		return c, err
	}
	max, err := r.plans.MaxVersion(ctx, key)
	if err != nil {
		return nil, err
	}
	if max == 0 {
		return nil, ErrNoTaskType
	}
	plan, err := r.plans.FindOneByPlanIdVersion(ctx, key, max)
	if err != nil {
		return nil, err
	}
	comp := &model.CfgComponent{Kind: string(KindTaskType), CompKey: key, LatestVersion: max, Status: "active", CreatedBy: plan.PublishedBy}
	if t, err := spec.Parse([]byte(plan.Dsl)); err == nil {
		comp.Name, comp.Namespace, comp.Description = t.Name, t.Namespace, t.Description
	}
	if _, err := r.components.Insert(ctx, comp); err != nil && !isDuplicate(err) {
		return nil, err
	}
	return r.components.FindOneByKindCompKey(ctx, string(KindTaskType), key)
}

func toDraft(key string, d *model.CfgDraft) *TaskTypeDraft {
	return &TaskTypeDraft{Key: key, BaseVersion: int(d.BaseVersion), Revision: d.Revision, Body: json.RawMessage(d.Body),
		ContentHash: ContentHash(json.RawMessage(d.Body)), UpdatedBy: d.UpdatedBy, UpdatedAt: d.UpdatedAt}
}

// ContentHash 计算去掉 ui 字段后的内容哈希（规范化的 JSON）：只移动画布坐标不改变哈希，试运行结果仍然有效。
func ContentHash(body json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	delete(m, "ui")
	b, _ := json.Marshal(m) // map 的键按字母序输出
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}
