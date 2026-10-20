// Package schema 嵌入任务类型 JSON 的结构契约，供 Go 校验与前端共用。
package schema

import _ "embed"

// TaskType 是 task_type.schema.json 的内容。
//
//go:embed task_type.schema.json
var TaskType []byte
