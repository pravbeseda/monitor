package evaluate_test

import (
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
)

// spec: timeline.md#model — a point stays fresh for three of its sensor's interval, or of
// the node's longest when the configuration gives that sensor none; a node running nothing
// keeps nothing fresh.
func TestLastingIsThreeOfTheIntervalAValueIsAgedBy(t *testing.T) {
	target := evaluate.Target{Intervals: map[string]time.Duration{"disk": 15 * time.Minute, "load": 5 * time.Minute}}
	for sensor, want := range map[string]time.Duration{
		"load":    15 * time.Minute,
		"disk":    45 * time.Minute,
		"":        45 * time.Minute,
		"systemd": 45 * time.Minute,
	} {
		if got := target.Lasting(sensor); got != want {
			t.Errorf("Lasting(%q) = %v, want %v", sensor, got, want)
		}
	}
	if got := (evaluate.Target{}).Lasting("disk"); got != 0 {
		t.Errorf("a node running nothing keeps a value fresh for %v", got)
	}
}
