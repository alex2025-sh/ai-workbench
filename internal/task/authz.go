package task

import "context"

// GroupChecker 判断用户是否属于某个用户组（directory.Groups 满足该接口）。
type GroupChecker interface {
	IsMember(ctx context.Context, group, userID string) (bool, error)
}

// CanDecide 报告 actor 能否对任务做流程决定（人工节点的选择）。
// Web 端的 RunService.Signal 与 Lark 卡片回调共用这一判断；引擎本身不鉴权。
//
// 处理人可以决定；任务未分配时，候选组成员无需先认领即可决定，做决定的人成为处理人。
// 任务的终态、节点是否在等待由引擎判断，这里不重复。
func CanDecide(ctx context.Context, t *Task, actor string, groups GroupChecker) (bool, error) {
	switch {
	case actor == "":
		return false, nil
	case t.AssigneeID != "":
		return t.AssigneeID == actor, nil
	case t.AssigneeGroup != "" && groups != nil:
		return groups.IsMember(ctx, t.AssigneeGroup, actor)
	default:
		return false, nil
	}
}
