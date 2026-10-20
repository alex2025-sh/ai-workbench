package tasktype

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RenderedCard 是渲染后的卡片，工作台、任务详情与 Lark 共用。
type RenderedCard struct {
	Title    string `json:"title"`
	NodeName string `json:"node_name,omitempty"` // 当前节点名，如「VIP 客服组长」
	Fields   []KV   `json:"fields,omitempty"`
	Link     *KV    `json:"link,omitempty"` // 节点启用 link 时的业务链接（label → url）
	TaskURL  string `json:"task_url,omitempty"`
}

// KV 是卡片上的一行。
type KV struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// DisplayLoc 是时间字段的显示时区。
var DisplayLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("UTC+8", 8*3600)
	}
	return loc
}()

// Values 是一条记录的字段值：字段名 → 值（SQL 结果或推送的 fields）。
type Values map[string]any

// DecodeValues 解码任务表中的 fields 列（数字用 json.Number，不经过 float64）。
func DecodeValues(raw []byte) Values {
	v := Values{}
	if len(raw) == 0 {
		return v
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	_ = dec.Decode(&v)
	return v
}

// Display 返回字段的显示值：按类型格式化，按 pii 脱敏。drop 的字段返回 false。
func (t *TaskType) Display(name string, key string, vals Values) (string, bool) {
	if name == KeyPlaceholder {
		return key, true
	}
	f, ok := t.Field(name)
	if !ok {
		return "", false
	}
	raw, ok := vals[name]
	if !ok || raw == nil {
		return "", true
	}
	s := format(f, raw)
	switch f.PII {
	case PIIDrop:
		return "", false
	case PIIHash:
		sum := sha256.Sum256([]byte(fmt.Sprint(raw)))
		return hex.EncodeToString(sum[:])[:12], true
	case PIIMaskMiddle:
		return maskMiddle(s), true
	}
	return s, true
}

func format(f Field, v any) string {
	s := fmt.Sprint(v)
	switch f.Type {
	case FieldDatetime:
		if t, ok := asTime(s); ok {
			return t.In(DisplayLoc).Format("2006-01-02 15:04")
		}
	case FieldEnum:
		for _, o := range f.Options {
			if o.Value == s {
				return o.Label
			}
		}
	}
	return s
}

// asTime 识别 Unix 秒、毫秒与 RFC3339 / MySQL datetime 文本。
func asTime(s string) (time.Time, bool) {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		switch {
		case n <= 0:
			return time.Time{}, false
		case n < 1e11:
			return time.Unix(n, 0), true
		default:
			return time.UnixMilli(n), true
		}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// maskMiddle 保留首尾各约四分之一，中间替换为 *，如 10000001 → 10****01。
func maskMiddle(s string) string {
	r := []rune(s)
	if len(r) <= 2 {
		return strings.Repeat("*", len(r))
	}
	keep := len(r) / 4
	if keep == 0 {
		keep = 1
	}
	return string(r[:keep]) + strings.Repeat("*", len(r)-2*keep) + string(r[len(r)-keep:])
}

// RenderCard 渲染某个节点上的卡片。taskURL 为工作台任务详情的地址，Lark 卡片的按钮用它。
func (t *TaskType) RenderCard(nodeID, key string, vals Values, taskURL string) RenderedCard {
	value := func(name string) (string, bool) { return t.Display(name, key, vals) }
	c := RenderedCard{Title: Render(t.Card.Title, value), TaskURL: taskURL}
	if n, _, ok := t.Node(nodeID); ok {
		c.NodeName = n.Name
		if n.Has(ActionLink) && n.Link != nil {
			c.Link = &KV{Label: n.Link.Label, Value: Render(n.Link.URL, value)}
		}
	}
	for _, name := range t.Card.Fields {
		v, show := value(name)
		if !show {
			continue
		}
		f, _ := t.Field(name)
		label := f.Label
		if label == "" {
			label = f.Name
		}
		c.Fields = append(c.Fields, KV{Label: label, Value: v})
	}
	return c
}

// Masked 返回全部字段的显示值（按字段顺序），任务详情的「业务字段」用。
func (t *TaskType) Masked(key string, vals Values) []KV {
	var out []KV
	for _, f := range t.Fields {
		v, show := t.Display(f.Name, key, vals)
		if !show {
			continue
		}
		label := f.Label
		if label == "" {
			label = f.Name
		}
		out = append(out, KV{Label: label, Value: v})
	}
	return out
}
