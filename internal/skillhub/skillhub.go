// Package skillhub 对接 Skillhub 的后端 API。
//
// 画布上的 skill_call 节点选择一个 skill，前端根据它的输入 schema 生成表单，
// 输出 schema 用于下游节点的变量引用与编译期类型检查。
package skillhub

import (
	"context"
	"encoding/json"
)

// Skill 是 Skillhub 中的一个 skill。
type Skill struct {
	Key         string
	Name        string
	Description string
	Version     string
}

// Schema 是 skill 的输入输出定义（JSON Schema）。
type Schema struct {
	Input  json.RawMessage
	Output json.RawMessage
}

// Client 是 Skillhub 客户端。
type Client interface {
	List(ctx context.Context, keyword string, pageToken string) ([]Skill, string, error)
	GetSchema(ctx context.Context, key, version string) (*Schema, error)
	// Invoke 执行 skill。idemKey 为 run_id:node_id:iteration[:seq]。
	Invoke(ctx context.Context, key, version string, input json.RawMessage, idemKey string) (json.RawMessage, error)
}
