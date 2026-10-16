package engine

import "testing"

func TestIdemKeyIncludesIteration(t *testing.T) {
	k0 := IdemKey(42, "ask_rm", 0, JobExecute)
	k1 := IdemKey(42, "ask_rm", 1, JobExecute)
	if k0 != "42:ask_rm:0:execute" {
		t.Fatalf("unexpected key %q", k0)
	}
	if k0 == k1 {
		t.Fatal("keys of different iterations must differ")
	}
}
