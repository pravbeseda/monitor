package uptime

import (
	"testing"
	"time"
)

// spec: host-sensors.md#uptime — the time since boot as the kernel publishes it.
func TestParseUptime(t *testing.T) {
	got, err := parse("266400.90 1000000.00\n")
	if err != nil || got != 266400*time.Second+900*time.Millisecond {
		t.Fatalf("parse = %v, %v; want 74h0m0.9s", got, err)
	}
}

// spec: host-sensors.md#uptime — a file that is not an uptime is an error.
func TestParseUptimeRefusesGarbage(t *testing.T) {
	for _, text := range []string{"", "up"} {
		if _, err := parse(text); err == nil {
			t.Errorf("parse(%q) succeeded, want an error", text)
		}
	}
}
