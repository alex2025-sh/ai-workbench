// Package mcp 把平台控制面暴露为 MCP 工具，供 Openclaw / Eino agent 调用。
//
// agent 只通过这些工具操作平台，不直接碰数据库与调度器；
// AI 生成的配置必须通过与人工配置相同的校验器，只能保存为草稿。
package mcp

// 平台 MCP 工具名。
const (
	ToolListSkills     = "list_skills"
	ToolGetSkillSchema = "get_skill_schema"
	ToolListTaskTypes  = "list_task_types"
	ToolListNodeTypes  = "list_node_types"
	ToolValidate       = "validate"
	ToolDryRun         = "dry_run"
	ToolSaveDraft      = "save_draft"
)
