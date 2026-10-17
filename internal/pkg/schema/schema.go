// Package schema 处理任务类型的 payload JSON Schema 及平台扩展关键字。
//
// format 是 JSON Schema 标准关键字；x- 开头的是平台扩展，标准校验器会忽略，只由平台读取：
//   - x-pii：敏感字段及脱敏方式，在 Lark、LLM、日志、跨部门推送等出口统一处理；
//   - x-format / x-currency-field：展示语义，如金额按币种格式化（金额在 payload 中为 string）；
//   - x-enum-source：值来自外部字典（如 aml_rules），用于校验、展示与给 LLM 的上下文；
//   - x-enum-labels：枚举值的中文含义，用于展示与给 LLM 的上下文。
//
// 字段的 title / description 同时用于表单标签、卡片字段名和 AI 摘要的字段说明。
package schema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// 扩展关键字。
const (
	KeyPII           = "x-pii"
	KeyFormat        = "x-format"
	KeyCurrencyField = "x-currency-field"
	KeyEnumSource    = "x-enum-source"
	KeyEnumLabels    = "x-enum-labels"
)

// PIIStrategy 是脱敏方式。
type PIIStrategy string

const (
	PIIMaskMiddle PIIStrategy = "mask_middle"
	PIIHash       PIIStrategy = "hash"
	PIIDrop       PIIStrategy = "drop"
)

// Format 是展示格式。
type Format string

const (
	FormatMoney    Format = "money"
	FormatPercent  Format = "percent"
	FormatDuration Format = "duration"
	FormatDatetime Format = "datetime" // Unix 毫秒或 RFC3339，按查看者时区展示
)

// Validator 按 task_type@version 的 schema 校验 payload。
// 解码 payload 时必须使用 json.Decoder.UseNumber()，金额不经过 float64。
type Validator interface {
	Validate(ctx context.Context, schemaRef string, payload json.RawMessage) error
}

// Audience 决定脱敏级别，如跨部门人员只能看到其环节所需字段。
type Audience struct {
	Dept  string
	Roles []string
}

// Masker 在出口处按 x-pii 对 payload 脱敏。
type Masker interface {
	Mask(ctx context.Context, schemaRef string, payload json.RawMessage, aud Audience) (json.RawMessage, error)
}

// EnumSource 提供 x-enum-source 字典的取值与业务含义。
type EnumSource interface {
	Label(ctx context.Context, source, value string) (label, description string, err error)
}

// Mask 按 payload schema 顶层字段（及嵌套 object）的 x-pii 返回脱敏后的副本，原值不变。
// 用于发往 Lark、邮件、LLM、日志等出口。schemaRaw 无法解析时原样返回。
func Mask(schemaRaw json.RawMessage, payload map[string]any) map[string]any {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schemaRaw, &s) != nil || payload == nil {
		return payload
	}
	return maskObject(s.Properties, payload)
}

func maskObject(props map[string]json.RawMessage, obj map[string]any) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		out[k] = v
		raw, ok := props[k]
		if !ok {
			continue
		}
		var f struct {
			PII        string                     `json:"x-pii"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(raw, &f) != nil {
			continue
		}
		switch PIIStrategy(f.PII) {
		case PIIDrop:
			delete(out, k)
		case PIIHash:
			sum := sha256.Sum256([]byte(fmt.Sprint(v)))
			out[k] = hex.EncodeToString(sum[:])[:12]
		case PIIMaskMiddle:
			out[k] = maskMiddle(fmt.Sprint(v))
		default:
			if sub, ok := v.(map[string]any); ok && len(f.Properties) > 0 {
				out[k] = maskObject(f.Properties, sub)
			}
		}
	}
	return out
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
