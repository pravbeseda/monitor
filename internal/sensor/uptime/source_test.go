package uptime_test

import (
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor/uptime"
)

// The platform source is thin but easy to break; this checks it against the machine the
// tests run on, whichever of the two supported platforms that is.
func TestSystemSourceReadsThisMachine(t *testing.T) {
	boot, err := uptime.System()()
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	if !boot.Before(time.Now()) || boot.Year() < 2000 {
		t.Fatalf("boot time %v, want a moment in the past", boot)
	}
}
