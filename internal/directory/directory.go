// Package directory 对接 BSP：人员、组织与接口级权限。
//
// 前端直接经 gateway 调用 BSP 选人选组织；后端只在分配策略解析处理人、
// 校验调用者权限时使用这里的接口。
package directory

import "context"

// User 是 BSP 中的人员。
type User struct {
	ID     string
	Name   string
	Email  string
	LarkID string
	DeptID string
	Active bool
}

// Directory 查询人员与组织。
type Directory interface {
	GetUser(ctx context.Context, id string) (*User, error)
	BatchGetUsers(ctx context.Context, ids []string) ([]User, error)
	DeptMembers(ctx context.Context, deptID string) ([]User, error)
	Manager(ctx context.Context, userID string) (*User, error)
}

// Authorizer 校验接口级权限（如 ai_workbench.task_type.publish）。
type Authorizer interface {
	Allowed(ctx context.Context, userID, permission string) (bool, error)
}
