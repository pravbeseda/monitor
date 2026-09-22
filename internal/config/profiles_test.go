package config_test

import (
	"maps"
	"slices"
	"testing"
	"time"
)

func withClass(class, extra string) string {
	return extra + `
nodes:
  laptop-a:
    class: ` + class + `
    token_env: MONITOR_TOKEN_LAPTOP_A
`
}

// intervals is what a node runs: each enabled sensor and its interval.
func intervals(t *testing.T, body string) map[string]time.Duration {
	t.Helper()
	out := map[string]time.Duration{}
	for name, s := range node(t, load(t, body), "laptop-a").Agent.Sensors {
		if s.Enabled {
			out[name] = s.Interval
		}
	}
	return out
}

// spec: hub-config.md#resolution — the compiled-in profiles and the host sensors' intervals.
func TestResolveCompiledInProfiles(t *testing.T) {
	tests := []struct {
		class string
		want  map[string]time.Duration
	}{
		{"server", map[string]time.Duration{
			"disk": 15 * time.Minute, "load": 5 * time.Minute, "memory": 5 * time.Minute,
			"uptime": 15 * time.Minute, "systemd": 15 * time.Minute,
		}},
		{"laptop", map[string]time.Duration{
			"disk": time.Hour, "load": 5 * time.Minute, "memory": 5 * time.Minute,
			"uptime": 15 * time.Minute,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			if got := intervals(t, withClass(tt.class, "")); !maps.Equal(got, tt.want) {
				t.Fatalf("sensors = %v, want %v", got, tt.want)
			}
		})
	}
}

// spec: hub-config.md#resolution — a profile in the file replaces the compiled-in one.
func TestResolveFileProfileReplacesTheCompiledIn(t *testing.T) {
	body := withClass("server", "classes:\n  server:\n    profile: [disk]\n")
	if got := slices.Collect(maps.Keys(intervals(t, body))); !slices.Equal(got, []string{"disk"}) {
		t.Fatalf("sensors = %v, want only disk", got)
	}
}

// spec: hub-config.md#resolution — a compiled-in interval below the tick is raised to it.
func TestResolveRaisesACompiledInIntervalToTheTick(t *testing.T) {
	body := withClass("laptop", "classes:\n  laptop:\n    base_tick: 1h\n")
	want := map[string]time.Duration{
		"disk": time.Hour, "load": time.Hour, "memory": time.Hour, "uptime": time.Hour,
	}
	if got := intervals(t, body); !maps.Equal(got, want) {
		t.Fatalf("sensors = %v, want %v", got, want)
	}
}
