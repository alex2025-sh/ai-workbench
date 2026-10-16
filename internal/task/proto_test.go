package task

import "testing"

func TestStatusProtoRoundTrip(t *testing.T) {
	want := map[TaskStatus]int32{
		TaskStatusUnassigned: 1,
		TaskStatusPending:    2,
		TaskStatusProcessing: 3,
		TaskStatusSnoozed:    4,
		TaskStatusSolved:     5,
	}
	for s, v := range want {
		if got := StatusToProto(s); got != v {
			t.Errorf("StatusToProto(%s) = %d, want %d", s, got, v)
		}
		back, ok := StatusFromProto(v)
		if !ok || back != s {
			t.Errorf("StatusFromProto(%d) = %s, %v; want %s", v, back, ok, s)
		}
	}
	if _, ok := StatusFromProto(0); ok {
		t.Error("UNSPECIFIED must not map to a status")
	}
	if _, ok := StatusFromProto(6); ok {
		t.Error("out-of-range value must not map to a status")
	}
}

func TestPriorityProto(t *testing.T) {
	if PriorityToProto(TaskPriorityP0) != 0 || PriorityToProto(TaskPriorityP3) != 3 {
		t.Error("priority proto values must equal stored values")
	}
	if _, ok := PriorityFromProto(4); ok {
		t.Error("out-of-range priority must be rejected")
	}
}
