package task

import "testing"

func TestStatusValuesAlignWithMorningBrief(t *testing.T) {
	cases := map[TaskStatus]int16{
		TaskStatusUnassigned: -1,
		TaskStatusPending:    0,
		TaskStatusProcessing: 1,
		TaskStatusSnoozed:    2,
		TaskStatusSolved:     3,
	}
	for s, want := range cases {
		if int16(s) != want {
			t.Errorf("%s = %d, want %d", s, int16(s), want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []TaskStatus{TaskStatusUnassigned, TaskStatusPending, TaskStatusProcessing, TaskStatusSnoozed} {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
	if !TaskStatusSolved.IsTerminal() {
		t.Error("solved should be terminal")
	}
}

func TestTransitions(t *testing.T) {
	allowed := []struct{ from, to TaskStatus }{
		{TaskStatusUnassigned, TaskStatusPending},    // 分配 / 认领
		{TaskStatusPending, TaskStatusProcessing},    // 开始处理
		{TaskStatusPending, TaskStatusSolved},        // 卡片上直接处理
		{TaskStatusPending, TaskStatusSnoozed},       // 推迟
		{TaskStatusSnoozed, TaskStatusPending},       // 到期回迁
		{TaskStatusProcessing, TaskStatusSolved},     // 完成
		{TaskStatusSnoozed, TaskStatusSolved},        // 源工单关闭，自动完结
		{TaskStatusProcessing, TaskStatusUnassigned}, // 退回队列
	}
	for _, c := range allowed {
		if err := CheckTransition(c.from, c.to); err != nil {
			t.Errorf("expected %s -> %s to be allowed: %v", c.from, c.to, err)
		}
	}

	denied := []struct{ from, to TaskStatus }{
		{TaskStatusSolved, TaskStatusPending},     // 终态不可回退
		{TaskStatusUnassigned, TaskStatusSnoozed}, // 没有处理人不能推迟
		{TaskStatusSnoozed, TaskStatusProcessing}, // 推迟到期先回到 pending
		{TaskStatusPending, TaskStatusPending},    // 转派不是状态转移
	}
	for _, c := range denied {
		if CanTransition(c.from, c.to) {
			t.Errorf("expected %s -> %s to be denied", c.from, c.to)
		}
	}
}

func TestPriorityValid(t *testing.T) {
	if !TaskPriorityP0.Valid() || !TaskPriorityP3.Valid() {
		t.Error("P0..P3 should be valid")
	}
	if TaskPriority(4).Valid() || TaskPriority(-1).Valid() {
		t.Error("out-of-range priority should be invalid")
	}
}
