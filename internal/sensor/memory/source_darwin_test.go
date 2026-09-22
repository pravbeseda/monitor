package memory

import "testing"

// spec: host-sensors.md#memory — the kernel's free level, of the installed memory.
func TestFromLevel(t *testing.T) {
	got, err := fromLevel(50, 16<<30)
	if err != nil || got.Pct != 50 || got.Bytes != 8589934592 {
		t.Fatalf("fromLevel = %+v, %v; want 50%% of 16 GiB", got, err)
	}
}

// spec: host-sensors.md#memory — a level outside 0..100 or no installed memory is an error.
func TestFromLevelRefusesNonsense(t *testing.T) {
	if _, err := fromLevel(101, 16<<30); err == nil {
		t.Error("level 101 accepted, want an error")
	}
	if _, err := fromLevel(56, 0); err == nil {
		t.Error("zero memsize accepted, want an error")
	}
}
