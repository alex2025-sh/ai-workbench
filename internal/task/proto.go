package task

// proto3 枚举的首值必须为 0，因此 workbench.proto 中的 TaskStatus 比存储值整体偏移 2
// （TASK_STATUS_UNSPECIFIED = 0，UNASSIGNED = 1 ... SOLVED = 5）。
// 以下函数在 gRPC 边界与领域值之间转换，避免各处手写偏移。
const protoStatusOffset = 2

// StatusToProto 把领域状态转换为 proto 枚举值。
func StatusToProto(s TaskStatus) int32 {
	if !s.Valid() {
		return 0 // TASK_STATUS_UNSPECIFIED
	}
	return int32(s) + protoStatusOffset
}

// StatusFromProto 把 proto 枚举值转换为领域状态；UNSPECIFIED 或越界返回 false。
func StatusFromProto(v int32) (TaskStatus, bool) {
	s := TaskStatus(v - protoStatusOffset)
	if v == 0 || !s.Valid() {
		return 0, false
	}
	return s, true
}

// PriorityToProto 与 PriorityFromProto：优先级的 proto 枚举与存储值一致（P0 = 0）。
func PriorityToProto(p TaskPriority) int32 { return int32(p) }

func PriorityFromProto(v int32) (TaskPriority, bool) {
	p := TaskPriority(v)
	return p, p.Valid()
}
